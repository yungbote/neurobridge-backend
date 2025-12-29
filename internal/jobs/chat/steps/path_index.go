package steps

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	chatIndex "github.com/yungbote/neurobridge-backend/internal/chat/index"
	"github.com/yungbote/neurobridge-backend/internal/clients/openai"
	pc "github.com/yungbote/neurobridge-backend/internal/clients/pinecone"
	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	chatrepo "github.com/yungbote/neurobridge-backend/internal/data/repos/chat"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type PathIndexDeps struct {
	DB *gorm.DB

	Log *logger.Logger
	AI  openai.Client
	Vec pc.VectorStore

	Docs repos.ChatDocRepo

	Path       repos.PathRepo
	PathNodes  repos.PathNodeRepo
	NodeActs   repos.PathNodeActivityRepo
	Activities repos.ActivityRepo
	Concepts   repos.ConceptRepo
}

type PathIndexInput struct {
	UserID uuid.UUID
	PathID uuid.UUID
}

type PathIndexOutput struct {
	DocsUpserted   int `json:"docs_upserted"`
	VectorUpserted int `json:"vector_upserted"`
}

// IndexPathDocsForChat rebuilds a compact retrieval projection of canonical path artifacts into chat_doc.
// This enables chat threads with thread.path_id to retrieve path context via normal hybrid retrieval.
func IndexPathDocsForChat(ctx context.Context, deps PathIndexDeps, in PathIndexInput) (PathIndexOutput, error) {
	out := PathIndexOutput{}
	if deps.DB == nil || deps.Log == nil || deps.AI == nil || deps.Docs == nil || deps.Path == nil || deps.PathNodes == nil {
		return out, fmt.Errorf("chat path index: missing deps")
	}
	if in.UserID == uuid.Nil || in.PathID == uuid.Nil {
		return out, fmt.Errorf("chat path index: missing ids")
	}

	dbc := dbctx.Context{Ctx: ctx, Tx: deps.DB}
	path, err := deps.Path.GetByID(dbc, in.PathID)
	if err != nil {
		return out, err
	}
	if path == nil || path.ID == uuid.Nil || path.UserID == nil || *path.UserID != in.UserID {
		return out, fmt.Errorf("path not found")
	}

	nodes, err := deps.PathNodes.GetByPathIDs(dbc, []uuid.UUID{in.PathID})
	if err != nil {
		return out, err
	}

	// Load activities (optional enrichment).
	var joins []*types.PathNodeActivity
	if deps.NodeActs != nil && len(nodes) > 0 {
		nodeIDs := make([]uuid.UUID, 0, len(nodes))
		for _, n := range nodes {
			if n != nil && n.ID != uuid.Nil {
				nodeIDs = append(nodeIDs, n.ID)
			}
		}
		if len(nodeIDs) > 0 {
			joins, _ = deps.NodeActs.GetByPathNodeIDs(dbc, nodeIDs)
		}
	}

	activityByID := map[uuid.UUID]*types.Activity{}
	if deps.Activities != nil && len(joins) > 0 {
		actIDs := make([]uuid.UUID, 0, len(joins))
		seen := map[uuid.UUID]bool{}
		for _, j := range joins {
			if j == nil || j.ActivityID == uuid.Nil || seen[j.ActivityID] {
				continue
			}
			seen[j.ActivityID] = true
			actIDs = append(actIDs, j.ActivityID)
			if len(actIDs) >= 400 {
				break
			}
		}
		if len(actIDs) > 0 {
			rows, _ := deps.Activities.GetByIDs(dbc, actIDs)
			for _, a := range rows {
				if a != nil && a.ID != uuid.Nil {
					activityByID[a.ID] = a
				}
			}
		}
	}

	// Load concepts (optional enrichment).
	var concepts []*types.Concept
	if deps.Concepts != nil {
		concepts, _ = deps.Concepts.GetByScope(dbc, "path", &in.PathID)
	}

	now := time.Now().UTC()
	ns := chatIndex.ChatUserNamespace(in.UserID)

	// Best-effort cleanup: remove previous path-scoped docs of these types (projection rebuild).
	docTypes := []string{DocTypePathOverview, DocTypePathNode, DocTypePathConcepts}
	var priorVectorIDs []string
	_ = deps.DB.WithContext(ctx).
		Model(&types.ChatDoc{}).
		Where("user_id = ? AND scope = ? AND scope_id = ? AND doc_type IN ?", in.UserID, ScopePath, in.PathID, docTypes).
		Pluck("vector_id", &priorVectorIDs).Error
	_ = deps.DB.WithContext(ctx).
		Where("user_id = ? AND scope = ? AND scope_id = ? AND doc_type IN ?", in.UserID, ScopePath, in.PathID, docTypes).
		Delete(&types.ChatDoc{}).Error
	if deps.Vec != nil && len(priorVectorIDs) > 0 {
		delCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		_ = deps.Vec.DeleteIDs(delCtx, ns, priorVectorIDs)
		cancel()
	}

	// Build docs.
	docs := make([]*types.ChatDoc, 0, 1+len(nodes))
	embedInputs := make([]string, 0, 1+len(nodes))

	overviewID := deterministicUUID(fmt.Sprintf("chat_doc|v%d|%s|path:%s|overview", ChatPathDocVersion, DocTypePathOverview, in.PathID.String()))
	overviewText := renderPathOverview(path, nodes, concepts)
	overview := &types.ChatDoc{
		ID:             overviewID,
		UserID:         in.UserID,
		DocType:        DocTypePathOverview,
		Scope:          ScopePath,
		ScopeID:        &in.PathID,
		ThreadID:       nil,
		PathID:         &in.PathID,
		JobID:          nil,
		SourceID:       &in.PathID,
		SourceSeq:      nil,
		ChunkIndex:     0,
		Text:           overviewText,
		ContextualText: "Learning path overview (retrieval context):\n" + overviewText,
		VectorID:       overviewID.String(),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	docs = append(docs, overview)
	embedInputs = append(embedInputs, overview.ContextualText)

	// Build node docs (stable order).
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i] == nil || nodes[j] == nil {
			return i < j
		}
		return nodes[i].Index < nodes[j].Index
	})

	joinsByNode := map[uuid.UUID][]*types.PathNodeActivity{}
	for _, j := range joins {
		if j == nil || j.PathNodeID == uuid.Nil {
			continue
		}
		joinsByNode[j.PathNodeID] = append(joinsByNode[j.PathNodeID], j)
	}

	for _, n := range nodes {
		if n == nil || n.ID == uuid.Nil {
			continue
		}
		nodeID := deterministicUUID(fmt.Sprintf("chat_doc|v%d|%s|path:%s|node:%s", ChatPathDocVersion, DocTypePathNode, in.PathID.String(), n.ID.String()))
		body := renderPathNode(n, joinsByNode[n.ID], activityByID)
		d := &types.ChatDoc{
			ID:             nodeID,
			UserID:         in.UserID,
			DocType:        DocTypePathNode,
			Scope:          ScopePath,
			ScopeID:        &in.PathID,
			ThreadID:       nil,
			PathID:         &in.PathID,
			JobID:          nil,
			SourceID:       &n.ID,
			SourceSeq:      nil,
			ChunkIndex:     0,
			Text:           body,
			ContextualText: "Path node (retrieval context):\n" + body,
			VectorID:       nodeID.String(),
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		docs = append(docs, d)
		embedInputs = append(embedInputs, d.ContextualText)
	}

	// Concepts doc (optional, capped).
	if len(concepts) > 0 {
		sort.Slice(concepts, func(i, j int) bool {
			if concepts[i] == nil || concepts[j] == nil {
				return i < j
			}
			if concepts[i].SortIndex != concepts[j].SortIndex {
				return concepts[i].SortIndex > concepts[j].SortIndex
			}
			return concepts[i].Depth < concepts[j].Depth
		})
		if len(concepts) > 120 {
			concepts = concepts[:120]
		}
		conceptsID := deterministicUUID(fmt.Sprintf("chat_doc|v%d|%s|path:%s|concepts", ChatPathDocVersion, DocTypePathConcepts, in.PathID.String()))
		body := renderPathConcepts(concepts)
		d := &types.ChatDoc{
			ID:             conceptsID,
			UserID:         in.UserID,
			DocType:        DocTypePathConcepts,
			Scope:          ScopePath,
			ScopeID:        &in.PathID,
			ThreadID:       nil,
			PathID:         &in.PathID,
			JobID:          nil,
			SourceID:       &in.PathID,
			SourceSeq:      nil,
			ChunkIndex:     0,
			Text:           body,
			ContextualText: "Path concepts (retrieval context):\n" + body,
			VectorID:       conceptsID.String(),
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		docs = append(docs, d)
		embedInputs = append(embedInputs, d.ContextualText)
	}

	// Embed in one shot (OpenAI client handles batching internally); fallback to empty embeddings if it fails.
	embs, err := deps.AI.Embed(ctx, embedInputs)
	if err != nil || len(embs) != len(docs) {
		embs = make([][]float32, len(docs))
	}
	for i := range docs {
		docs[i].Embedding = datatypes.JSON(chatrepo.MustEmbeddingJSON(nonNilEmb(embs[i])))
	}

	if err := deps.Docs.Upsert(dbc, docs); err != nil {
		return out, err
	}
	out.DocsUpserted = len(docs)

	if deps.Vec != nil {
		_ = upsertVectors(ctx, deps.Vec, ns, docs, embs)
		out.VectorUpserted = len(docs)
	}

	return out, nil
}

func renderPathOverview(path *types.Path, nodes []*types.PathNode, concepts []*types.Concept) string {
	if path == nil {
		return ""
	}
	title := strings.TrimSpace(path.Title)
	if title == "" {
		title = "Learning path"
	}
	desc := strings.TrimSpace(path.Description)
	status := strings.TrimSpace(path.Status)
	if status == "" {
		status = "unknown"
	}

	var b strings.Builder
	b.WriteString("Path: " + title + "\n")
	b.WriteString("PathID: " + path.ID.String() + "\n")
	b.WriteString("Status: " + status + "\n")
	if desc != "" {
		b.WriteString("Description: " + desc + "\n")
	}
	if len(nodes) > 0 {
		b.WriteString("\nNodes:\n")
		for _, n := range nodes {
			if n == nil || n.ID == uuid.Nil {
				continue
			}
			b.WriteString(fmt.Sprintf("- [%d] %s (node_id=%s)\n", n.Index, strings.TrimSpace(n.Title), n.ID.String()))
		}
	}
	if len(concepts) > 0 {
		b.WriteString("\nConcepts (high-level):\n")
		// take top ~30 for overview; full list is its own doc
		sort.Slice(concepts, func(i, j int) bool {
			if concepts[i] == nil || concepts[j] == nil {
				return i < j
			}
			return concepts[i].SortIndex > concepts[j].SortIndex
		})
		if len(concepts) > 30 {
			concepts = concepts[:30]
		}
		for _, c := range concepts {
			if c == nil || strings.TrimSpace(c.Name) == "" {
				continue
			}
			b.WriteString("- " + strings.TrimSpace(c.Name) + "\n")
		}
	}
	return strings.TrimSpace(b.String())
}

func renderPathNode(n *types.PathNode, joins []*types.PathNodeActivity, activityByID map[uuid.UUID]*types.Activity) string {
	if n == nil {
		return ""
	}
	title := strings.TrimSpace(n.Title)
	if title == "" {
		title = "Node"
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Node %d: %s\n", n.Index, title))
	b.WriteString("NodeID: " + n.ID.String() + "\n")
	b.WriteString("PathID: " + n.PathID.String() + "\n")
	if n.ParentNodeID != nil && *n.ParentNodeID != uuid.Nil {
		b.WriteString("ParentNodeID: " + n.ParentNodeID.String() + "\n")
	}
	if len(n.Gating) > 0 && string(n.Gating) != "null" {
		b.WriteString("Gating: " + trimToChars(string(n.Gating), 800) + "\n")
	}

	if len(joins) > 0 {
		b.WriteString("\nActivities:\n")
		// joins already ordered by repo (path_node_id ASC, is_primary DESC, rank ASC)
		count := 0
		for _, j := range joins {
			if j == nil || j.ActivityID == uuid.Nil {
				continue
			}
			a := activityByID[j.ActivityID]
			name := j.ActivityID.String()
			kind := ""
			status := ""
			if a != nil {
				if strings.TrimSpace(a.Title) != "" {
					name = strings.TrimSpace(a.Title)
				}
				kind = strings.TrimSpace(a.Kind)
				status = strings.TrimSpace(a.Status)
			}
			line := "- "
			if j.IsPrimary {
				line += "[primary] "
			}
			line += fmt.Sprintf("%s (activity_id=%s", name, j.ActivityID.String())
			if kind != "" {
				line += " kind=" + kind
			}
			if status != "" {
				line += " status=" + status
			}
			line += ")\n"
			b.WriteString(line)
			count++
			if count >= 12 {
				break
			}
		}
	}

	return strings.TrimSpace(b.String())
}

func renderPathConcepts(concepts []*types.Concept) string {
	var b strings.Builder
	b.WriteString("Concepts:\n")
	for _, c := range concepts {
		if c == nil {
			continue
		}
		name := strings.TrimSpace(c.Name)
		if name == "" {
			continue
		}
		key := strings.TrimSpace(c.Key)
		summary := strings.TrimSpace(c.Summary)
		if summary != "" {
			summary = trimToChars(summary, 280)
		}
		line := "- " + name
		if key != "" {
			line += " (key=" + key + ")"
		}
		if summary != "" {
			line += ": " + summary
		}
		b.WriteString(line + "\n")
	}
	return strings.TrimSpace(b.String())
}
