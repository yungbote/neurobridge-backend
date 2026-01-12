package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	chatrepo "github.com/yungbote/neurobridge-backend/internal/data/repos/chat"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	chatIndex "github.com/yungbote/neurobridge-backend/internal/modules/chat/index"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/neo4jdb"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
	pc "github.com/yungbote/neurobridge-backend/internal/platform/pinecone"
)

type MaintainDeps struct {
	DB *gorm.DB

	Log *logger.Logger
	AI  openai.Client
	Vec pc.VectorStore

	Graph *neo4jdb.Client

	Threads   repos.ChatThreadRepo
	Messages  repos.ChatMessageRepo
	State     repos.ChatThreadStateRepo
	Summaries repos.ChatSummaryNodeRepo

	Docs     repos.ChatDocRepo
	Memory   repos.ChatMemoryItemRepo
	Entities repos.ChatEntityRepo
	Edges    repos.ChatEdgeRepo
	Claims   repos.ChatClaimRepo
}

type MaintainInput struct {
	UserID   uuid.UUID
	ThreadID uuid.UUID
}

func MaintainThread(ctx context.Context, deps MaintainDeps, in MaintainInput) error {
	if deps.DB == nil || deps.Log == nil || deps.AI == nil || deps.Threads == nil || deps.Messages == nil || deps.State == nil || deps.Docs == nil {
		return fmt.Errorf("chat maintain: missing deps")
	}
	if in.UserID == uuid.Nil || in.ThreadID == uuid.Nil {
		return fmt.Errorf("chat maintain: missing ids")
	}

	dbc := dbctx.Context{Ctx: ctx, Tx: deps.DB}
	threadRows, err := deps.Threads.GetByIDs(dbc, []uuid.UUID{in.ThreadID})
	if err != nil {
		return err
	}
	if len(threadRows) == 0 || threadRows[0] == nil {
		return fmt.Errorf("thread not found")
	}
	thread := threadRows[0]
	if thread.UserID != in.UserID {
		return fmt.Errorf("thread not found")
	}

	state, err := deps.State.GetOrCreate(dbc, in.ThreadID)
	if err != nil {
		return err
	}

	if err := indexNewMessages(ctx, deps, thread, state); err != nil {
		return err
	}
	if err := updateRaptor(ctx, deps, thread, state); err != nil {
		return err
	}
	if err := updateGraph(ctx, deps, thread, state); err != nil {
		return err
	}
	if err := updateMemory(ctx, deps, thread, state); err != nil {
		return err
	}

	// Best-effort: sync derived chat graph into Neo4j for cross-thread graph queries.
	if deps.Graph != nil {
		if err := syncChatThreadToNeo4j(ctx, deps, thread); err != nil {
			deps.Log.Warn("neo4j chat graph sync failed (continuing)", "error", err, "thread_id", thread.ID.String())
		}
		if err := syncChatTurnProvenanceToNeo4j(ctx, deps, thread); err != nil {
			deps.Log.Warn("neo4j chat turn provenance sync failed (continuing)", "error", err, "thread_id", thread.ID.String())
		}
	}

	return nil
}

func indexNewMessages(ctx context.Context, deps MaintainDeps, thread *types.ChatThread, state *types.ChatThreadState) error {
	dbc := dbctx.Context{Ctx: ctx, Tx: deps.DB}
	msgs, err := deps.Messages.ListSinceSeq(dbc, thread.ID, state.LastIndexedSeq, 500)
	if err != nil {
		return err
	}
	if len(msgs) == 0 {
		return nil
	}

	// Hot context for contextualization.
	recentMsgs, _ := deps.Messages.ListRecent(dbc, thread.ID, 16)
	recent := formatRecent(recentMsgs, 12)

	chunkChars := 2200
	ns := chatIndex.ChatUserNamespace(thread.UserID)

	type unit struct {
		doc *types.ChatDoc
		emb []float32
	}

	var (
		mu    sync.Mutex
		units []unit
	)

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(8)

	for _, m := range msgs {
		m := m
		if m == nil || strings.TrimSpace(m.Content) == "" {
			continue
		}
		chunks := chunkByChars(m.Content, chunkChars)
		for idx, chunk := range chunks {
			idx, chunk := idx, strings.TrimSpace(chunk)
			if chunk == "" {
				continue
			}
			g.Go(func() error {
				sys, usr := promptContextualizeChunk(thread.Title, m.Role, chunk, recent)
				ctxText := chunk
				if obj, err := deps.AI.GenerateJSON(gctx, sys, usr, "chat_contextualize_chunk", schemaContextualizeChunk()); err == nil {
					if s := strings.TrimSpace(asString(obj["contextual_text"])); s != "" {
						ctxText = s
					}
				}

				var emb []float32
				if embs, err := deps.AI.Embed(gctx, []string{ctxText}); err == nil && len(embs) > 0 {
					emb = embs[0]
				}

				docID := deterministicUUID(fmt.Sprintf("chat_doc|v%d|%s|%s|%d", ChatChunkVersion, DocTypeMessageChunk, m.ID.String(), idx))
				seq := m.Seq
				doc := &types.ChatDoc{
					ID:     docID,
					UserID: thread.UserID,

					DocType: DocTypeMessageChunk,
					Scope:   ScopeThread,
					ScopeID: &thread.ID,

					ThreadID: &thread.ID,
					PathID:   thread.PathID,
					JobID:    thread.JobID,

					SourceID:   &m.ID,
					SourceSeq:  &seq,
					ChunkIndex: idx,

					Text:           chunk,
					ContextualText: ctxText,

					Embedding: datatypes.JSON(chatrepo.MustEmbeddingJSON(nonNilEmb(emb))),
					VectorID:  docID.String(),
					CreatedAt: m.CreatedAt,
					UpdatedAt: nowUTC(),
				}
				mu.Lock()
				units = append(units, unit{doc: doc, emb: emb})
				mu.Unlock()
				return nil
			})
		}
	}

	if err := g.Wait(); err != nil {
		return err
	}

	if len(units) == 0 {
		state.LastIndexedSeq = msgs[len(msgs)-1].Seq
		return deps.State.UpdateFields(dbc, thread.ID, map[string]interface{}{
			"last_indexed_seq": state.LastIndexedSeq,
		})
	}

	docs := make([]*types.ChatDoc, 0, len(units))
	embs := make([][]float32, 0, len(units))
	for _, u := range units {
		docs = append(docs, u.doc)
		embs = append(embs, nonNilEmb(u.emb))
	}

	if err := deps.Docs.Upsert(dbc, docs); err != nil {
		return err
	}
	// Best-effort Pinecone upsert (retrieval cache).
	if deps.Vec != nil {
		_ = upsertVectors(ctx, deps.Vec, ns, docs, embs)
	}

	state.LastIndexedSeq = msgs[len(msgs)-1].Seq
	return deps.State.UpdateFields(dbc, thread.ID, map[string]interface{}{
		"last_indexed_seq": state.LastIndexedSeq,
	})
}

func updateRaptor(ctx context.Context, deps MaintainDeps, thread *types.ChatThread, state *types.ChatThreadState) error {
	if deps.Summaries == nil {
		return nil
	}

	dbc := dbctx.Context{Ctx: ctx, Tx: deps.DB}
	msgs, err := deps.Messages.ListSinceSeq(dbc, thread.ID, state.LastSummarizedSeq, 1000)
	if err != nil {
		return err
	}
	if len(msgs) == 0 {
		return nil
	}

	// leaf windows
	const leafSize = 20
	leaves := make([]*types.ChatSummaryNode, 0)
	for i := 0; i < len(msgs); {
		end := i + leafSize
		if end > len(msgs) {
			end = len(msgs)
		}
		window := msgs[i:end]
		if len(window) == 0 {
			break
		}
		startSeq := window[0].Seq
		endSeq := window[len(window)-1].Seq
		windowText := formatWindow(window)

		leafID := deterministicUUID(fmt.Sprintf("chat_summary_node|v%d|thread:%s|level:%d|%d-%d", ChatSummaryVersion, thread.ID.String(), 0, startSeq, endSeq))
		// Avoid summary drift: if this node already exists, reuse it.
		{
			var existing types.ChatSummaryNode
			if err := deps.DB.WithContext(ctx).
				Model(&types.ChatSummaryNode{}).
				Where("id = ?", leafID).
				First(&existing).Error; err == nil && existing.ID != uuid.Nil && strings.TrimSpace(existing.SummaryMD) != "" {
				leaves = append(leaves, &existing)
				i = end
				continue
			}
		}
		smd := ""
		{
			sys, usr := promptSummarizeNode(0, windowText)
			if obj, err := deps.AI.GenerateJSON(ctx, sys, usr, "chat_summary_node", schemaSummarizeNode()); err == nil {
				smd = strings.TrimSpace(asString(obj["summary_md"]))
			}
		}
		if smd == "" {
			smd = "- (summary unavailable)"
		}
		leaf := &types.ChatSummaryNode{
			ID:           leafID,
			ThreadID:     thread.ID,
			ParentID:     nil,
			Level:        0,
			StartSeq:     startSeq,
			EndSeq:       endSeq,
			SummaryMD:    smd,
			ChildNodeIDs: datatypes.JSON([]byte(`[]`)),
		}
		leaves = append(leaves, leaf)
		i = end
	}

	if err := deps.Summaries.Create(dbc, leaves); err != nil {
		return err
	}

	// docs for leaves
	ns := chatIndex.ChatUserNamespace(thread.UserID)
	leafDocs := make([]*types.ChatDoc, 0, len(leaves))
	leafEmbIn := make([]string, 0, len(leaves))
	for _, n := range leaves {
		if n == nil {
			continue
		}
		docID := deterministicUUID("chat_doc|" + DocTypeSummary + "|" + n.ID.String())
		seq := n.EndSeq
		d := &types.ChatDoc{
			ID:     docID,
			UserID: thread.UserID,

			DocType: DocTypeSummary,
			Scope:   ScopeThread,
			ScopeID: &thread.ID,

			ThreadID: &thread.ID,
			PathID:   thread.PathID,
			JobID:    thread.JobID,

			SourceID:   &n.ID,
			SourceSeq:  &seq,
			ChunkIndex: 0,

			Text:           n.SummaryMD,
			ContextualText: "RAPTOR leaf summary:\n" + n.SummaryMD,
			VectorID:       docID.String(),
			CreatedAt:      n.CreatedAt,
			UpdatedAt:      nowUTC(),
		}
		leafDocs = append(leafDocs, d)
		leafEmbIn = append(leafEmbIn, d.ContextualText)
	}

	leafEmb, err := deps.AI.Embed(ctx, leafEmbIn)
	if err != nil {
		// Keep summaries usable for lexical retrieval even if embeddings fail.
		leafEmb = make([][]float32, len(leafDocs))
	}
	for i := range leafDocs {
		leafDocs[i].Embedding = datatypes.JSON(chatrepo.MustEmbeddingJSON(nonNilEmb(leafEmb[i])))
	}
	if err := deps.Docs.Upsert(dbc, leafDocs); err != nil {
		return err
	}
	if deps.Vec != nil {
		_ = upsertVectors(ctx, deps.Vec, ns, leafDocs, leafEmb)
	}

	// Build upper levels incrementally.
	if err := buildUpperRaptorLevels(ctx, deps, thread); err != nil {
		return err
	}

	state.LastSummarizedSeq = msgs[len(msgs)-1].Seq
	return deps.State.UpdateFields(dbc, thread.ID, map[string]interface{}{
		"last_summarized_seq": state.LastSummarizedSeq,
	})
}

func buildUpperRaptorLevels(ctx context.Context, deps MaintainDeps, thread *types.ChatThread) error {
	dbc := dbctx.Context{Ctx: ctx, Tx: deps.DB}
	const maxLevels = 12
	clusterSize := 8

	for level := 0; level < maxLevels; level++ {
		nodes, err := deps.Summaries.ListOrphansByLevel(dbc, thread.ID, level)
		if err != nil {
			return err
		}
		if len(nodes) <= 1 {
			break
		}

		// Embed node summaries for clustering.
		embIn := make([]string, 0, len(nodes))
		for _, n := range nodes {
			if n != nil {
				embIn = append(embIn, n.SummaryMD)
			}
		}
		embs, err := deps.AI.Embed(ctx, embIn)
		if err != nil {
			return err
		}

		// Determine k (clusters).
		k := int(math.Ceil(float64(len(nodes)) / float64(clusterSize)))
		assign := kmeansCosine(embs, k, 10)

		clusters := make([][]*types.ChatSummaryNode, k)
		for i, c := range assign {
			clusters[c] = append(clusters[c], nodes[i])
		}

		for _, cluster := range clusters {
			if len(cluster) == 0 {
				continue
			}
			if err := makeParentNode(ctx, deps, thread, level+1, cluster); err != nil {
				return err
			}
		}
	}

	return nil
}

func makeParentNode(ctx context.Context, deps MaintainDeps, thread *types.ChatThread, parentLevel int, children []*types.ChatSummaryNode) error {
	dbc := dbctx.Context{Ctx: ctx, Tx: deps.DB}
	sort.Slice(children, func(i, j int) bool { return children[i].StartSeq < children[j].StartSeq })
	start := children[0].StartSeq
	end := children[len(children)-1].EndSeq

	childIDs := make([]string, 0, len(children))
	var childSummaries strings.Builder
	for _, c := range children {
		if c == nil {
			continue
		}
		childIDs = append(childIDs, c.ID.String())
		childSummaries.WriteString("- [" + c.ID.String() + "] (seq " + itoa64(c.StartSeq) + "-" + itoa64(c.EndSeq) + ")\n" + c.SummaryMD + "\n\n")
	}
	sort.Strings(childIDs)
	childIDsJSON, _ := json.Marshal(childIDs)

	parentID := deterministicUUID(fmt.Sprintf("chat_summary_node|v%d|thread:%s|level:%d|children:%s", ChatSummaryVersion, thread.ID.String(), parentLevel, strings.Join(childIDs, ",")))
	var smd string
	// Avoid summary drift: if this parent already exists, reuse it.
	{
		var existing types.ChatSummaryNode
		if err := deps.DB.WithContext(ctx).
			Model(&types.ChatSummaryNode{}).
			Where("id = ?", parentID).
			First(&existing).Error; err == nil && existing.ID != uuid.Nil && strings.TrimSpace(existing.SummaryMD) != "" {
			smd = strings.TrimSpace(existing.SummaryMD)
		}
	}
	if smd == "" {
		sys, usr := promptSummarizeNode(parentLevel, childSummaries.String())
		if obj, err := deps.AI.GenerateJSON(ctx, sys, usr, "chat_summary_node", schemaSummarizeNode()); err == nil {
			smd = strings.TrimSpace(asString(obj["summary_md"]))
		}
		if smd == "" {
			smd = "- (summary unavailable)"
		}
	}
	parent := &types.ChatSummaryNode{
		ID:           parentID,
		ThreadID:     thread.ID,
		ParentID:     nil,
		Level:        parentLevel,
		StartSeq:     start,
		EndSeq:       end,
		SummaryMD:    smd,
		ChildNodeIDs: datatypes.JSON(childIDsJSON),
	}

	// Atomically ensure parent exists and children are linked.
	if err := deps.DB.WithContext(ctx).Transaction(func(txx *gorm.DB) error {
		dbc := dbctx.Context{Ctx: ctx, Tx: txx}
		if err := deps.Summaries.Create(dbc, []*types.ChatSummaryNode{parent}); err != nil {
			return err
		}
		childUUIDs := make([]uuid.UUID, 0, len(children))
		for _, c := range children {
			if c != nil {
				childUUIDs = append(childUUIDs, c.ID)
			}
		}
		if err := deps.Summaries.SetParent(dbc, childUUIDs, parent.ID); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return err
	}

	// Create doc for parent summary.
	docID := deterministicUUID("chat_doc|" + DocTypeSummary + "|" + parent.ID.String())
	seq := parent.EndSeq
	d := &types.ChatDoc{
		ID:             docID,
		UserID:         thread.UserID,
		DocType:        DocTypeSummary,
		Scope:          ScopeThread,
		ScopeID:        &thread.ID,
		ThreadID:       &thread.ID,
		PathID:         thread.PathID,
		JobID:          thread.JobID,
		SourceID:       &parent.ID,
		SourceSeq:      &seq,
		Text:           parent.SummaryMD,
		ContextualText: fmt.Sprintf("RAPTOR summary level %d:\n%s", parentLevel, parent.SummaryMD),
		VectorID:       docID.String(),
		CreatedAt:      parent.CreatedAt,
		UpdatedAt:      nowUTC(),
	}
	embs, err := deps.AI.Embed(ctx, []string{d.ContextualText})
	if err == nil && len(embs) > 0 {
		d.Embedding = datatypes.JSON(chatrepo.MustEmbeddingJSON(nonNilEmb(embs[0])))
	} else {
		d.Embedding = datatypes.JSON(chatrepo.MustEmbeddingJSON([]float32{}))
	}
	if err := deps.Docs.Upsert(dbc, []*types.ChatDoc{d}); err != nil {
		return err
	}
	if deps.Vec != nil && len(embs) > 0 {
		_ = upsertVectors(ctx, deps.Vec, chatIndex.ChatUserNamespace(thread.UserID), []*types.ChatDoc{d}, embs[:1])
	}
	return nil
}

func updateGraph(ctx context.Context, deps MaintainDeps, thread *types.ChatThread, state *types.ChatThreadState) error {
	if deps.Entities == nil || deps.Edges == nil || deps.Claims == nil {
		return nil
	}
	dbc := dbctx.Context{Ctx: ctx, Tx: deps.DB}
	msgs, err := deps.Messages.ListSinceSeq(dbc, thread.ID, state.LastGraphSeq, 300)
	if err != nil {
		return err
	}
	if len(msgs) == 0 {
		return nil
	}
	window := formatWindow(msgs)

	sys, usr := promptGraphExtract(thread.Title, window)
	obj, err := deps.AI.GenerateJSON(ctx, sys, usr, "chat_graph_extract", schemaGraphExtract())
	if err != nil {
		// Graph extraction is derived; avoid blocking maintenance if the model misbehaves.
		state.LastGraphSeq = msgs[len(msgs)-1].Seq
		if err := deps.State.UpdateFields(dbc, thread.ID, map[string]interface{}{
			"last_graph_seq": state.LastGraphSeq,
		}); err != nil {
			return err
		}
		return nil
	}

	entitiesAny, _ := obj["entities"].([]any)
	relationsAny, _ := obj["relations"].([]any)
	claimsAny, _ := obj["claims"].([]any)

	nameToID := map[string]uuid.UUID{}
	ns := chatIndex.ChatUserNamespace(thread.UserID)

	// Upsert entities.
	for _, ea := range entitiesAny {
		m, _ := ea.(map[string]any)
		name := strings.TrimSpace(asString(m["name"]))
		if name == "" {
			continue
		}
		etype := strings.TrimSpace(asString(m["type"]))
		if etype == "" {
			etype = "unknown"
		}
		desc := strings.TrimSpace(asString(m["description"]))
		aliasesJSON, _ := json.Marshal(m["aliases"])

		row := &types.ChatEntity{
			UserID:      thread.UserID,
			Scope:       ScopeThread,
			ScopeID:     &thread.ID,
			ThreadID:    &thread.ID,
			PathID:      thread.PathID,
			JobID:       thread.JobID,
			Name:        name,
			Type:        etype,
			Description: desc,
			Aliases:     datatypes.JSON(aliasesJSON),
			UpdatedAt:   nowUTC(),
		}
		up, err := deps.Entities.UpsertByName(dbc, row)
		if err != nil {
			return err
		}
		nameToID[strings.ToLower(name)] = up.ID

		// Entity doc.
		docID := deterministicUUID("chat_doc|" + DocTypeEntity + "|" + up.ID.String())
		text := "Entity: " + name + "\nType: " + etype + "\nDescription: " + desc
		doc := &types.ChatDoc{
			ID:             docID,
			UserID:         thread.UserID,
			DocType:        DocTypeEntity,
			Scope:          ScopeThread,
			ScopeID:        &thread.ID,
			ThreadID:       &thread.ID,
			PathID:         thread.PathID,
			JobID:          thread.JobID,
			SourceID:       &up.ID,
			Text:           text,
			ContextualText: "Graph entity:\n" + text,
			VectorID:       docID.String(),
			CreatedAt:      nowUTC(),
			UpdatedAt:      nowUTC(),
		}
		embs, err := deps.AI.Embed(ctx, []string{doc.ContextualText})
		if err == nil && len(embs) > 0 {
			doc.Embedding = datatypes.JSON(chatrepo.MustEmbeddingJSON(nonNilEmb(embs[0])))
		} else {
			doc.Embedding = datatypes.JSON(chatrepo.MustEmbeddingJSON([]float32{}))
		}
		if err := deps.Docs.Upsert(dbc, []*types.ChatDoc{doc}); err != nil {
			return err
		}
		if deps.Vec != nil && len(embs) > 0 {
			_ = upsertVectors(ctx, deps.Vec, ns, []*types.ChatDoc{doc}, embs[:1])
		}
	}

	// Relations -> edges (best-effort insert; dedupe can be added later).
	for _, ra := range relationsAny {
		m, _ := ra.(map[string]any)
		src := strings.ToLower(strings.TrimSpace(asString(m["src"])))
		dst := strings.ToLower(strings.TrimSpace(asString(m["dst"])))
		rel := strings.TrimSpace(asString(m["relation"]))
		if src == "" || dst == "" || rel == "" {
			continue
		}
		srcID, ok1 := nameToID[src]
		dstID, ok2 := nameToID[dst]
		if !ok1 || !ok2 {
			continue
		}
		w := asFloat(m["weight"])
		if w <= 0 {
			w = 0.5
		}
		evJSON, _ := json.Marshal(m["evidence_seqs"])
		edgeID := deterministicUUID(fmt.Sprintf("chat_edge|v%d|user:%s|scope:%s|scope_id:%s|src:%s|dst:%s|rel:%s", ChatGraphVersion, thread.UserID.String(), ScopeThread, thread.ID.String(), srcID.String(), dstID.String(), rel))
		edge := &types.ChatEdge{
			ID:           edgeID,
			UserID:       thread.UserID,
			Scope:        ScopeThread,
			ScopeID:      &thread.ID,
			SrcEntityID:  srcID,
			DstEntityID:  dstID,
			Relation:     rel,
			Weight:       w,
			EvidenceSeqs: datatypes.JSON(evJSON),
			CreatedAt:    nowUTC(),
		}
		_ = deps.Edges.Create(dbc, []*types.ChatEdge{edge})
	}

	// Claims -> rows + docs.
	for _, ca := range claimsAny {
		m, _ := ca.(map[string]any)
		content := strings.TrimSpace(asString(m["content"]))
		if content == "" {
			continue
		}
		enJSON, _ := json.Marshal(m["entity_names"])
		evJSON, _ := json.Marshal(m["evidence_seqs"])

		claimID := deterministicUUID("chat_claim|" + thread.ID.String() + "|" + content)
		claim := &types.ChatClaim{
			ID:           claimID,
			UserID:       thread.UserID,
			Scope:        ScopeThread,
			ScopeID:      &thread.ID,
			ThreadID:     &thread.ID,
			PathID:       thread.PathID,
			JobID:        thread.JobID,
			Content:      content,
			EntityNames:  datatypes.JSON(enJSON),
			EvidenceSeqs: datatypes.JSON(evJSON),
			CreatedAt:    nowUTC(),
		}
		_ = deps.Claims.InsertIgnore(dbc, []*types.ChatClaim{claim})

		docID := deterministicUUID("chat_doc|" + DocTypeClaim + "|" + claimID.String())
		text := "Claim: " + content
		doc := &types.ChatDoc{
			ID:             docID,
			UserID:         thread.UserID,
			DocType:        DocTypeClaim,
			Scope:          ScopeThread,
			ScopeID:        &thread.ID,
			ThreadID:       &thread.ID,
			PathID:         thread.PathID,
			JobID:          thread.JobID,
			SourceID:       &claimID,
			Text:           text,
			ContextualText: "Graph claim:\n" + text,
			VectorID:       docID.String(),
			CreatedAt:      nowUTC(),
			UpdatedAt:      nowUTC(),
		}
		embs, err := deps.AI.Embed(ctx, []string{doc.ContextualText})
		if err == nil && len(embs) > 0 {
			doc.Embedding = datatypes.JSON(chatrepo.MustEmbeddingJSON(nonNilEmb(embs[0])))
		} else {
			doc.Embedding = datatypes.JSON(chatrepo.MustEmbeddingJSON([]float32{}))
		}
		if err := deps.Docs.Upsert(dbc, []*types.ChatDoc{doc}); err != nil {
			return err
		}
		if deps.Vec != nil && len(embs) > 0 {
			_ = upsertVectors(ctx, deps.Vec, ns, []*types.ChatDoc{doc}, embs[:1])
		}
	}

	state.LastGraphSeq = msgs[len(msgs)-1].Seq
	return deps.State.UpdateFields(dbc, thread.ID, map[string]interface{}{
		"last_graph_seq": state.LastGraphSeq,
	})
}

func updateMemory(ctx context.Context, deps MaintainDeps, thread *types.ChatThread, state *types.ChatThreadState) error {
	if deps.Memory == nil {
		return nil
	}

	dbc := dbctx.Context{Ctx: ctx, Tx: deps.DB}
	msgs, err := deps.Messages.ListSinceSeq(dbc, thread.ID, state.LastMemorySeq, 250)
	if err != nil {
		return err
	}
	if len(msgs) == 0 {
		return nil
	}
	window := formatWindow(msgs)

	sys, usr := promptMemoryExtract(thread.Title, window)
	obj, err := deps.AI.GenerateJSON(ctx, sys, usr, "chat_memory_extract", schemaMemoryExtract())
	if err != nil {
		state.LastMemorySeq = msgs[len(msgs)-1].Seq
		if err := deps.State.UpdateFields(dbc, thread.ID, map[string]interface{}{
			"last_memory_seq": state.LastMemorySeq,
		}); err != nil {
			return err
		}
		return nil
	}
	itemsAny, _ := obj["items"].([]any)
	if len(itemsAny) == 0 {
		state.LastMemorySeq = msgs[len(msgs)-1].Seq
		return deps.State.UpdateFields(dbc, thread.ID, map[string]interface{}{
			"last_memory_seq": state.LastMemorySeq,
		})
	}

	var rows []*types.ChatMemoryItem
	for _, ia := range itemsAny {
		m, _ := ia.(map[string]any)
		kind := strings.TrimSpace(asString(m["kind"]))
		scope := strings.TrimSpace(asString(m["scope"]))
		key := strings.TrimSpace(asString(m["key"]))
		val := strings.TrimSpace(asString(m["value"]))
		if key == "" || val == "" || kind == "" || scope == "" {
			continue
		}
		conf := asFloat(m["confidence"])
		evJSON, _ := json.Marshal(m["evidence_seqs"])

		row := &types.ChatMemoryItem{
			UserID:       thread.UserID,
			Kind:         kind,
			Scope:        scope,
			Key:          key,
			Value:        val,
			Confidence:   conf,
			EvidenceSeqs: datatypes.JSON(evJSON),
			UpdatedAt:    nowUTC(),
			CreatedAt:    nowUTC(),
		}

		switch scope {
		case ScopeThread:
			row.ScopeID = &thread.ID
			row.ThreadID = &thread.ID
			row.PathID = thread.PathID
			row.JobID = thread.JobID
		case ScopePath:
			if thread.PathID == nil || *thread.PathID == uuid.Nil {
				continue
			}
			row.ScopeID = thread.PathID
			row.ThreadID = &thread.ID
			row.PathID = thread.PathID
			row.JobID = thread.JobID
		case ScopeUser:
			row.ScopeID = nil
			row.ThreadID = &thread.ID
			row.PathID = thread.PathID
			row.JobID = thread.JobID
		default:
			continue
		}

		rows = append(rows, row)
	}

	if len(rows) == 0 {
		state.LastMemorySeq = msgs[len(msgs)-1].Seq
		return deps.State.UpdateFields(dbc, thread.ID, map[string]interface{}{
			"last_memory_seq": state.LastMemorySeq,
		})
	}

	if err := deps.Memory.UpsertMany(dbc, rows); err != nil {
		return err
	}

	// Create/update docs for memory items.
	ns := chatIndex.ChatUserNamespace(thread.UserID)
	memDocs := make([]*types.ChatDoc, 0, len(rows))
	memEmbIn := make([]string, 0, len(rows))
	for _, r := range rows {
		if r == nil || r.ID == uuid.Nil {
			continue
		}
		docID := deterministicUUID("chat_doc|" + DocTypeMemory + "|" + r.ID.String())
		text := "Memory (" + r.Kind + "): " + r.Key + " = " + r.Value
		d := &types.ChatDoc{
			ID:             docID,
			UserID:         thread.UserID,
			DocType:        DocTypeMemory,
			Scope:          r.Scope,
			ScopeID:        r.ScopeID,
			ThreadID:       r.ThreadID,
			PathID:         r.PathID,
			JobID:          r.JobID,
			SourceID:       &r.ID,
			Text:           text,
			ContextualText: "Durable memory item:\n" + text,
			VectorID:       docID.String(),
			CreatedAt:      r.UpdatedAt,
			UpdatedAt:      nowUTC(),
		}
		memDocs = append(memDocs, d)
		memEmbIn = append(memEmbIn, d.ContextualText)
	}

	memEmb, err := deps.AI.Embed(ctx, memEmbIn)
	if err != nil {
		memEmb = make([][]float32, len(memDocs))
	}
	for i := range memDocs {
		memDocs[i].Embedding = datatypes.JSON(chatrepo.MustEmbeddingJSON(nonNilEmb(memEmb[i])))
	}
	if err := deps.Docs.Upsert(dbc, memDocs); err != nil {
		return err
	}
	if deps.Vec != nil {
		_ = upsertVectors(ctx, deps.Vec, ns, memDocs, memEmb)
	}

	state.LastMemorySeq = msgs[len(msgs)-1].Seq
	return deps.State.UpdateFields(dbc, thread.ID, map[string]interface{}{
		"last_memory_seq": state.LastMemorySeq,
	})
}
