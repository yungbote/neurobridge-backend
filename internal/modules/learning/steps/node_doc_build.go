package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/content"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/content/schema"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/index"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/gcp"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
	pc "github.com/yungbote/neurobridge-backend/internal/platform/pinecone"
	"github.com/yungbote/neurobridge-backend/internal/services"
	"golang.org/x/sync/errgroup"
)

type NodeDocBuildDeps struct {
	DB  *gorm.DB
	Log *logger.Logger

	Path      repos.PathRepo
	PathNodes repos.PathNodeRepo
	NodeDocs  repos.LearningNodeDocRepo
	Figures   repos.LearningNodeFigureRepo
	Videos    repos.LearningNodeVideoRepo
	GenRuns   repos.LearningDocGenerationRunRepo

	Files  repos.MaterialFileRepo
	Chunks repos.MaterialChunkRepo

	UserProfile      repos.UserProfileVectorRepo
	TeachingPatterns repos.TeachingPatternRepo

	AI  openai.Client
	Vec pc.VectorStore

	Bucket gcp.BucketService

	Bootstrap services.LearningBuildBootstrapService
}

type NodeDocBuildInput struct {
	OwnerUserID   uuid.UUID
	MaterialSetID uuid.UUID
	SagaID        uuid.UUID
}

type NodeDocBuildOutput struct {
	PathID       uuid.UUID `json:"path_id"`
	DocsWritten  int       `json:"docs_written"`
	DocsExisting int       `json:"docs_existing"`

	// Aggregate quality/shape metrics for the docs written in this run.
	DiagramsWritten int `json:"diagrams_written"`
	FiguresWritten  int `json:"figures_written"`
	VideosWritten   int `json:"videos_written"`
	TablesWritten   int `json:"tables_written"`
}

const nodeDocPromptVersion = "node_doc_v1@4"

func NodeDocBuild(ctx context.Context, deps NodeDocBuildDeps, in NodeDocBuildInput) (NodeDocBuildOutput, error) {
	out := NodeDocBuildOutput{}
	if deps.DB == nil || deps.Log == nil || deps.Path == nil || deps.PathNodes == nil || deps.NodeDocs == nil || deps.Files == nil || deps.Chunks == nil || deps.UserProfile == nil || deps.AI == nil || deps.Bootstrap == nil {
		return out, fmt.Errorf("node_doc_build: missing deps")
	}
	if in.OwnerUserID == uuid.Nil {
		return out, fmt.Errorf("node_doc_build: missing owner_user_id")
	}
	if in.MaterialSetID == uuid.Nil {
		return out, fmt.Errorf("node_doc_build: missing material_set_id")
	}

	pathID, err := deps.Bootstrap.EnsurePath(dbctx.Context{Ctx: ctx}, in.OwnerUserID, in.MaterialSetID)
	if err != nil {
		return out, err
	}
	out.PathID = pathID

	// Safety: don't break legacy installs where migrations haven't created the new tables yet.
	if !deps.DB.Migrator().HasTable(&types.LearningNodeDoc{}) {
		deps.Log.Warn("learning_node_doc table missing; skipping node_doc_build (RUN_MIGRATIONS?)")
		return out, nil
	}

	up, err := deps.UserProfile.GetByUserID(dbctx.Context{Ctx: ctx}, in.OwnerUserID)
	if err != nil || up == nil || strings.TrimSpace(up.ProfileDoc) == "" {
		return out, fmt.Errorf("node_doc_build: missing user_profile_doc (run user_profile_refresh first)")
	}
	userProfileDoc := strings.TrimSpace(up.ProfileDoc)

	pathRow, err := deps.Path.GetByID(dbctx.Context{Ctx: ctx}, pathID)
	if err != nil {
		return out, err
	}
	pathStyleJSON := ""
	pathIntentMD := ""
	var allowFiles map[uuid.UUID]bool
	if pathRow != nil && len(pathRow.Metadata) > 0 && string(pathRow.Metadata) != "null" {
		var meta map[string]any
		if json.Unmarshal(pathRow.Metadata, &meta) == nil {
			if v, ok := meta["intake_md"]; ok && v != nil {
				pathIntentMD = strings.TrimSpace(stringFromAny(v))
			}
			allowFiles = intakeMaterialAllowlistFromPathMeta(meta)
			if v, ok := meta["charter"]; ok && v != nil {
				// Extract just the stable style fields (avoid charter warnings like "ask 2-4 questions").
				if charter, ok := v.(map[string]any); ok {
					if ps, ok := charter["path_style"]; ok && ps != nil {
						if pb, err := json.Marshal(ps); err == nil {
							pathStyleJSON = string(pb)
						}
					}
				}
			}
		}
	}

	nodes, err := deps.PathNodes.GetByPathIDs(dbctx.Context{Ctx: ctx}, []uuid.UUID{pathID})
	if err != nil {
		return out, err
	}
	if len(nodes) == 0 {
		return out, fmt.Errorf("node_doc_build: no path nodes (run path_plan_build first)")
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Index < nodes[j].Index })

	nodeIDs := make([]uuid.UUID, 0, len(nodes))
	for _, n := range nodes {
		if n != nil && n.ID != uuid.Nil {
			nodeIDs = append(nodeIDs, n.ID)
		}
	}

	existingDocs, err := deps.NodeDocs.GetByPathNodeIDs(dbctx.Context{Ctx: ctx}, nodeIDs)
	if err != nil {
		return out, err
	}
	hasDoc := map[uuid.UUID]bool{}
	for _, d := range existingDocs {
		if d != nil && d.PathNodeID != uuid.Nil {
			hasDoc[d.PathNodeID] = true
		}
	}

	// Optional: generated figures (raster) that can be referenced by unit docs.
	figChunkIDsByNode := map[uuid.UUID][]uuid.UUID{}
	figAssetsByNode := map[uuid.UUID][]*mediaAssetCandidate{}
	if deps.Figures != nil && deps.DB.Migrator().HasTable(&types.LearningNodeFigure{}) {
		rows, ferr := deps.Figures.GetByPathNodeIDs(dbctx.Context{Ctx: ctx}, nodeIDs)
		if ferr == nil && len(rows) > 0 {
			for _, r := range rows {
				if r == nil || r.PathNodeID == uuid.Nil || r.Slot <= 0 {
					continue
				}
				status := strings.ToLower(strings.TrimSpace(r.Status))
				if status != "rendered" {
					continue
				}
				url := strings.TrimSpace(r.AssetURL)
				if url == "" && deps.Bucket != nil && strings.TrimSpace(r.AssetStorageKey) != "" {
					url = deps.Bucket.GetPublicURL(gcp.BucketCategoryMaterial, strings.TrimSpace(r.AssetStorageKey))
				}
				if url == "" {
					continue
				}

				// Parse plan JSON for caption/alt/citations (best-effort).
				var plan content.FigurePlanItemV1
				if len(r.PlanJSON) > 0 && string(r.PlanJSON) != "null" {
					_ = json.Unmarshal(r.PlanJSON, &plan)
				}

				// Expose as an available media asset for NodeDoc generator.
				fn := fmt.Sprintf("figure_slot_%d.png", r.Slot)
				if strings.TrimSpace(plan.SemanticType) != "" {
					fn = fmt.Sprintf("%s_slot_%d.png", strings.TrimSpace(plan.SemanticType), r.Slot)
				}
				notesParts := make([]string, 0, 4)
				if strings.TrimSpace(plan.SemanticType) != "" {
					notesParts = append(notesParts, "semantic_type="+strings.TrimSpace(plan.SemanticType))
				}
				if strings.TrimSpace(plan.PlacementHint) != "" {
					notesParts = append(notesParts, "placement_hint="+shorten(strings.TrimSpace(plan.PlacementHint), 160))
				}
				if strings.TrimSpace(plan.Caption) != "" {
					notesParts = append(notesParts, "caption="+shorten(strings.TrimSpace(plan.Caption), 180))
				}

				cids := make([]string, 0, len(plan.Citations))
				for _, s := range plan.Citations {
					s = strings.TrimSpace(s)
					if s == "" {
						continue
					}
					cids = append(cids, s)
					if id, e := uuid.Parse(s); e == nil && id != uuid.Nil {
						figChunkIDsByNode[r.PathNodeID] = append(figChunkIDsByNode[r.PathNodeID], id)
					}
				}
				figChunkIDsByNode[r.PathNodeID] = dedupeUUIDsPreserveOrder(figChunkIDsByNode[r.PathNodeID])

				mime := strings.TrimSpace(r.AssetMimeType)
				if mime == "" {
					mime = "image/png"
				}

				figAssetsByNode[r.PathNodeID] = append(figAssetsByNode[r.PathNodeID], &mediaAssetCandidate{
					Kind:      "image",
					URL:       url,
					Key:       strings.TrimSpace(r.AssetStorageKey),
					Notes:     strings.Join(notesParts, " | "),
					ChunkIDs:  dedupeStrings(cids),
					FileName:  fn,
					MimeType:  mime,
					Source:    "derived",
					AssetKind: "generated_figure",
				})
			}
		}
	}

	// Optional: generated videos that can be referenced by unit docs.
	vidChunkIDsByNode := map[uuid.UUID][]uuid.UUID{}
	vidAssetsByNode := map[uuid.UUID][]*mediaAssetCandidate{}
	if deps.Videos != nil && deps.DB.Migrator().HasTable(&types.LearningNodeVideo{}) {
		rows, verr := deps.Videos.GetByPathNodeIDs(dbctx.Context{Ctx: ctx}, nodeIDs)
		if verr == nil && len(rows) > 0 {
			for _, r := range rows {
				if r == nil || r.PathNodeID == uuid.Nil || r.Slot <= 0 {
					continue
				}
				status := strings.ToLower(strings.TrimSpace(r.Status))
				if status != "rendered" {
					continue
				}
				url := strings.TrimSpace(r.AssetURL)
				if url == "" && deps.Bucket != nil && strings.TrimSpace(r.AssetStorageKey) != "" {
					url = deps.Bucket.GetPublicURL(gcp.BucketCategoryMaterial, strings.TrimSpace(r.AssetStorageKey))
				}
				if url == "" {
					continue
				}

				// Parse plan JSON for caption/alt/citations (best-effort).
				var plan content.VideoPlanItemV1
				if len(r.PlanJSON) > 0 && string(r.PlanJSON) != "null" {
					_ = json.Unmarshal(r.PlanJSON, &plan)
				}

				fn := fmt.Sprintf("video_slot_%d.mp4", r.Slot)
				if strings.TrimSpace(plan.SemanticType) != "" {
					fn = fmt.Sprintf("%s_slot_%d.mp4", strings.TrimSpace(plan.SemanticType), r.Slot)
				}
				notesParts := make([]string, 0, 4)
				if strings.TrimSpace(plan.SemanticType) != "" {
					notesParts = append(notesParts, "semantic_type="+strings.TrimSpace(plan.SemanticType))
				}
				if strings.TrimSpace(plan.PlacementHint) != "" {
					notesParts = append(notesParts, "placement_hint="+shorten(strings.TrimSpace(plan.PlacementHint), 160))
				}
				if strings.TrimSpace(plan.Caption) != "" {
					notesParts = append(notesParts, "caption="+shorten(strings.TrimSpace(plan.Caption), 180))
				}

				cids := make([]string, 0, len(plan.Citations))
				for _, s := range plan.Citations {
					s = strings.TrimSpace(s)
					if s == "" {
						continue
					}
					cids = append(cids, s)
					if id, e := uuid.Parse(s); e == nil && id != uuid.Nil {
						vidChunkIDsByNode[r.PathNodeID] = append(vidChunkIDsByNode[r.PathNodeID], id)
					}
				}
				vidChunkIDsByNode[r.PathNodeID] = dedupeUUIDsPreserveOrder(vidChunkIDsByNode[r.PathNodeID])

				mime := strings.TrimSpace(r.AssetMimeType)
				if mime == "" {
					mime = "video/mp4"
				}

				vidAssetsByNode[r.PathNodeID] = append(vidAssetsByNode[r.PathNodeID], &mediaAssetCandidate{
					Kind:      "video",
					URL:       url,
					Key:       strings.TrimSpace(r.AssetStorageKey),
					Notes:     strings.Join(notesParts, " | "),
					ChunkIDs:  dedupeStrings(cids),
					FileName:  fn,
					MimeType:  mime,
					Source:    "derived",
					AssetKind: "generated_video",
				})
			}
		}
	}

	files, err := deps.Files.GetByMaterialSetID(dbctx.Context{Ctx: ctx}, in.MaterialSetID)
	if err != nil {
		return out, err
	}
	if len(allowFiles) > 0 {
		filtered := filterMaterialFilesByAllowlist(files, allowFiles)
		if len(filtered) > 0 {
			files = filtered
		} else {
			deps.Log.Warn("node_doc_build: intake filter excluded all files; ignoring filter", "path_id", pathID.String())
		}
	}
	fileIDs := make([]uuid.UUID, 0, len(files))
	for _, f := range files {
		if f != nil && f.ID != uuid.Nil {
			fileIDs = append(fileIDs, f.ID)
		}
	}
	allChunks, err := deps.Chunks.GetByMaterialFileIDs(dbctx.Context{Ctx: ctx}, fileIDs)
	if err != nil {
		return out, err
	}
	if len(allChunks) == 0 {
		return out, fmt.Errorf("node_doc_build: no chunks for material set")
	}

	chunkByID := map[uuid.UUID]*types.MaterialChunk{}
	for _, ch := range allChunks {
		if ch == nil || ch.ID == uuid.Nil {
			continue
		}
		if isUnextractableChunk(ch) {
			continue
		}
		chunkByID[ch.ID] = ch
	}

	// ---- Deterministic coverage tracking (ensure every chunk is cited across the path) ----
	allChunkIDs := make([]uuid.UUID, 0, len(chunkByID))
	for id := range chunkByID {
		allChunkIDs = append(allChunkIDs, id)
	}
	sort.Slice(allChunkIDs, func(i, j int) bool { return allChunkIDs[i].String() < allChunkIDs[j].String() })

	coveredChunkIDs := map[uuid.UUID]bool{}
	for _, d := range existingDocs {
		if d == nil || len(d.DocJSON) == 0 || string(d.DocJSON) == "null" {
			continue
		}
		for _, s := range content.CitedChunkIDsFromNodeDocJSON([]byte(d.DocJSON)) {
			if id, err := uuid.Parse(strings.TrimSpace(s)); err == nil && id != uuid.Nil {
				coveredChunkIDs[id] = true
			}
		}
	}
	uncoveredChunkIDs := make([]uuid.UUID, 0)
	for _, id := range allChunkIDs {
		if id == uuid.Nil {
			continue
		}
		if coveredChunkIDs[id] {
			continue
		}
		uncoveredChunkIDs = append(uncoveredChunkIDs, id)
	}

	buildExcerpts := func(ids []uuid.UUID, maxLines int, maxChars int) string {
		if maxLines <= 0 {
			maxLines = 18
		}
		if maxChars <= 0 {
			maxChars = 850
		}
		var b strings.Builder
		n := 0
		seen := map[uuid.UUID]bool{}
		for _, id := range ids {
			if id == uuid.Nil || seen[id] {
				continue
			}
			seen[id] = true
			ch := chunkByID[id]
			if ch == nil {
				continue
			}
			txt := shorten(ch.Text, maxChars)
			if strings.TrimSpace(txt) == "" {
				continue
			}
			b.WriteString("[chunk_id=")
			b.WriteString(id.String())
			b.WriteString("] ")
			b.WriteString(txt)
			b.WriteString("\n")
			n++
			if n >= maxLines {
				break
			}
		}
		return strings.TrimSpace(b.String())
	}

	// Lazy-build deterministic local-embedding scan order for semantic fallback retrieval.
	// This avoids decoding every chunk embedding when Pinecone (or lexical) already returns enough evidence.
	var (
		chunkEmbsOnce sync.Once
		chunkEmbs     []chunkEmbedding
		chunkEmbsErr  error
	)
	buildChunkEmbs := func() ([]chunkEmbedding, error) {
		chunkEmbsOnce.Do(func() {
			tmp := make([]chunkEmbedding, 0, len(chunkByID))
			for id, ch := range chunkByID {
				if id == uuid.Nil || ch == nil {
					continue
				}
				if v, ok := decodeEmbedding(ch.Embedding); ok && len(v) > 0 {
					tmp = append(tmp, chunkEmbedding{ID: id, Emb: v})
				}
			}
			if len(tmp) == 0 {
				chunkEmbsErr = fmt.Errorf("node_doc_build: no local embeddings available (run embed_chunks first)")
				return
			}
			sort.Slice(tmp, func(i, j int) bool { return tmp[i].ID.String() < tmp[j].ID.String() })
			chunkEmbs = tmp
		})
		return chunkEmbs, chunkEmbsErr
	}

	type nodeWork struct {
		Node        *types.PathNode
		NodeKind    string
		DocTemplate string
		Goal        string
		ConceptCSV  string
		QueryText   string
		QueryEmb    []float32
	}
	work := make([]nodeWork, 0, len(nodes))
	for _, node := range nodes {
		if node == nil || node.ID == uuid.Nil {
			continue
		}
		if hasDoc[node.ID] {
			out.DocsExisting++
			continue
		}

		nodeMeta := map[string]any{}
		if len(node.Metadata) > 0 && string(node.Metadata) != "null" {
			_ = json.Unmarshal(node.Metadata, &nodeMeta)
		}
		nodeKind := normalizePathNodeKind(stringFromAny(nodeMeta["node_kind"]))
		docTemplate := normalizePathNodeDocTemplate(stringFromAny(nodeMeta["doc_template"]), nodeKind)
		nodeGoal := strings.TrimSpace(stringFromAny(nodeMeta["goal"]))
		nodeConceptKeys := dedupeStrings(stringSliceFromAny(nodeMeta["concept_keys"]))
		conceptCSV := strings.Join(nodeConceptKeys, ", ")

		queryText := strings.TrimSpace(node.Title + " " + nodeGoal + " " + conceptCSV)

		work = append(work, nodeWork{
			Node:        node,
			NodeKind:    nodeKind,
			DocTemplate: docTemplate,
			Goal:        nodeGoal,
			ConceptCSV:  conceptCSV,
			QueryText:   queryText,
		})
	}
	if len(work) == 0 {
		return out, nil
	}

	// Batch query embeddings to minimize API calls.
	queryTexts := make([]string, 0, len(work))
	for _, w := range work {
		queryTexts = append(queryTexts, w.QueryText)
	}
	queryEmbs, err := deps.AI.Embed(ctx, queryTexts)
	if err != nil {
		return out, err
	}
	if len(queryEmbs) != len(work) {
		return out, fmt.Errorf("node_doc_build: embedding count mismatch (got %d want %d)", len(queryEmbs), len(work))
	}
	for i := range work {
		work[i].QueryEmb = queryEmbs[i]
		if len(work[i].QueryEmb) == 0 {
			return out, fmt.Errorf("node_doc_build: empty query embedding")
		}
	}

	// Distribute uncovered chunk IDs across docs-to-build so that every chunk is cited at least once.
	// This is bounded per node for prompt size; increase NODE_DOC_MUST_CITE_PER_NODE for larger material sets.
	mustCiteByNodeID := map[uuid.UUID][]uuid.UUID{}
	if len(uncoveredChunkIDs) > 0 {
		perNode := envInt("NODE_DOC_MUST_CITE_PER_NODE", 2)
		if perNode < 1 {
			perNode = 1
		}
		if perNode > 8 {
			perNode = 8
		}
		needed := (len(uncoveredChunkIDs) + len(work) - 1) / len(work)
		if needed > perNode {
			perNode = needed
		}
		if perNode > 10 {
			perNode = 10
		}

		counts := make([]int, len(work))
		for _, cid := range uncoveredChunkIDs {
			ch := chunkByID[cid]
			var emb []float32
			ok := false
			if ch != nil {
				emb, ok = decodeEmbedding(ch.Embedding)
			}

			best := 0
			bestScore := -1.0
			if ok && len(emb) > 0 {
				for i := range work {
					s := cosineSim(work[i].QueryEmb, emb)
					if s > bestScore || (s == bestScore && counts[i] < counts[best]) {
						best = i
						bestScore = s
					}
				}
			} else {
				for i := 1; i < len(work); i++ {
					if counts[i] < counts[best] {
						best = i
					}
				}
			}

			// Enforce per-node cap by choosing the least-loaded node with remaining capacity.
			if counts[best] >= perNode {
				alt := -1
				for i := range work {
					if counts[i] < perNode && (alt == -1 || counts[i] < counts[alt]) {
						alt = i
					}
				}
				if alt != -1 {
					best = alt
				}
			}

			nid := uuid.Nil
			if work[best].Node != nil {
				nid = work[best].Node.ID
			}
			if nid == uuid.Nil {
				continue
			}
			mustCiteByNodeID[nid] = append(mustCiteByNodeID[nid], cid)
			counts[best]++
		}
		for nid := range mustCiteByNodeID {
			sort.Slice(mustCiteByNodeID[nid], func(i, j int) bool {
				return mustCiteByNodeID[nid][i].String() < mustCiteByNodeID[nid][j].String()
			})
		}
	}

	docSchema, err := schema.NodeDocGenV1()
	if err != nil {
		return out, err
	}

	chunksNS := index.ChunksNamespace(in.MaterialSetID)

	maxConc := envInt("NODE_DOC_BUILD_CONCURRENCY", 4)
	if maxConc < 1 {
		maxConc = 1
	}

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConc)

	var written int32
	var diagrams int32
	var figures int32
	var videos int32
	var tables int32

	for i := range work {
		w := work[i]
		g.Go(func() error {
			if w.Node == nil || w.Node.ID == uuid.Nil {
				return nil
			}

			// ---- Evidence retrieval (semantic + lexical + fallback) ----
			const semanticK = 22
			const lexicalK = 10
			const finalK = 22

			var retrieved []uuid.UUID
			if deps.Vec != nil {
				ids, qerr := deps.Vec.QueryIDs(gctx, chunksNS, w.QueryEmb, semanticK, pineconeChunkFilterWithAllowlist(allowFiles))
				if qerr == nil && len(ids) > 0 {
					for _, s := range ids {
						if id, e := uuid.Parse(strings.TrimSpace(s)); e == nil && id != uuid.Nil {
							retrieved = append(retrieved, id)
						}
					}
				}
			}

			lexIDs, _ := lexicalChunkIDs(dbctx.Context{Ctx: gctx, Tx: deps.DB}, fileIDs, w.QueryText, lexicalK)
			retrieved = append(retrieved, lexIDs...)
			retrieved = dedupeUUIDsPreserveOrder(retrieved)

			if len(retrieved) < finalK {
				ce, err := buildChunkEmbs()
				if err != nil {
					return err
				}
				fallback := topKChunkIDsByCosine(w.QueryEmb, ce, finalK)
				retrieved = dedupeUUIDsPreserveOrder(append(retrieved, fallback...))
			}

			// Ensure full coverage: include any must-cite chunks assigned to this node (placed first so they appear in excerpts).
			mustCiteIDs := mustCiteByNodeID[w.Node.ID]
			figCiteIDs := figChunkIDsByNode[w.Node.ID]
			vidCiteIDs := vidChunkIDsByNode[w.Node.ID]
			chunkIDs := mergeUUIDListsPreserveOrder(mustCiteIDs, figCiteIDs, vidCiteIDs, retrieved)
			if len(chunkIDs) > finalK {
				chunkIDs = chunkIDs[:finalK]
			}

			excerpts := buildExcerpts(chunkIDs, 20, 900)
			if strings.TrimSpace(excerpts) == "" {
				return fmt.Errorf("node_doc_build: empty grounding excerpts")
			}

			extras := make([]*mediaAssetCandidate, 0, len(figAssetsByNode[w.Node.ID])+len(vidAssetsByNode[w.Node.ID]))
			extras = append(extras, figAssetsByNode[w.Node.ID]...)
			extras = append(extras, vidAssetsByNode[w.Node.ID]...)
			assetsJSON := buildAvailableAssetsJSON(deps.Bucket, files, chunkByID, chunkIDs, extras)

			allowedChunkIDs := map[string]bool{}
			for _, id := range chunkIDs {
				allowedChunkIDs[id.String()] = true
			}
			mustCiteIDsAllowed := make([]uuid.UUID, 0, len(mustCiteIDs))
			for _, id := range mustCiteIDs {
				if id == uuid.Nil {
					continue
				}
				if allowedChunkIDs[id.String()] {
					mustCiteIDsAllowed = append(mustCiteIDsAllowed, id)
				}
			}

			// ---- Learner-facing doc with validation + retry ----
			reqs := nodeDocRequirementsForTemplate(w.NodeKind, w.DocTemplate)
			diagramLimit := envIntAllowZero("NODE_DOC_DIAGRAMS_LIMIT", -1)
			if diagramLimit < 0 {
				diagramLimit = -1
			}
			diagramsDisabled := diagramLimit == 0
			if diagramsDisabled {
				reqs.MinDiagrams = 0
			}
			hasGeneratedFigures := len(figAssetsByNode[w.Node.ID]) > 0
			// Optional: if a video is available in the allowed assets list, suggest it (the model may include it if helpful).
			videoAsset := firstVideoAssetFromAssetsJSON(assetsJSON)

			requireDiagrams := !diagramsDisabled && reqs.MinDiagrams > 0

			mediaRequirementLine := "- Media blocks (figure/diagram/table) are optional; include only if they materially improve learning."
			if reqs.RequireMedia {
				if diagramsDisabled {
					mediaRequirementLine = "- Must include at least one of: figure | table"
				} else {
					mediaRequirementLine = "- Must include at least one of: figure | diagram | table"
				}
			}

			generatedFigureRequirementLine := ""
			if hasGeneratedFigures {
				generatedFigureRequirementLine = `- GENERATED_FIGURE_ASSETS is available; include a figure block using one of those URLs only if it genuinely helps (optional).`
			}

			citationsRequirementLine := "- Every content block (everything except heading/divider/video/code) must have citations (non-empty)"
			outlineDiagramLine := "- Diagrams are optional (include one only if it genuinely improves clarity)."
			diagramRuleLine := "- Diagrams are optional."
			diagramPrefRule := ""
			templateRequirementLine := docTemplateRequirementLine(w.DocTemplate, diagramsDisabled)
			if diagramsDisabled {
				outlineDiagramLine = "- Do not include any diagram blocks."
				diagramRuleLine = "- Do not include any diagram blocks."
			} else if requireDiagrams {
				outlineDiagramLine = "- Include at least one diagram early."
				diagramRuleLine = "- Include at least one diagram block (SVG preferred)."
				diagramPrefRule = `- Prefer diagram.kind="svg" (simple, readable SVG).`
			}

			system := fmt.Sprintf(`
	MODE: STATIC_UNIT_DOC

	You write course-quality unit lessons for self-study: clear narrative, vivid intuition, a mental model, worked examples, and retrieval practice.
	The quality bar is elite: engaging and teacherly — not terse "lecture notes".
	This is NOT an interactive chat: do not ask the learner any questions or solicit preferences.
	Do not include any onboarding sections ("Entry Check", "Your goal/level", "Format preferences", etc).
	The only questions in the entire doc must be inside quick_check blocks.

		Teaching style rules:
		- Write in a direct, friendly, confident voice (like a great tutor).
		- Use paragraphs for explanation and transitions; bullets/tables are support, not the entire lesson.
		- Early in the lesson, include a short "Roadmap" section that previews the structure (3-6 bullets). If a diagram would help orientation, include a simple diagram block near the roadmap.
		- Use intentional repetition only as brief recap; do not copy/paste or restate the same line multiple times.
		- Be concrete: prefer a small toy example before abstraction when possible.
		- Do not include filler/boilerplate (e.g., repeating "The key idea is..." in every section).
		- Do not output raw concept keys (snake_case). Write concept names in natural language.
	- Summary is already provided via the summary field; do NOT include a "Summary" heading/section that repeats it.
	  If you include an ending recap, title it "Key takeaways" and make it additive (not a rephrase).

	Pedagogy:
	- If TEACHING_PATTERNS_JSON is provided, pick 1-2 patterns and apply them (without naming keys) so the lesson teaches, not a page of facts.
	- Spread quick checks throughout the doc (not all at the end).

		Media rules (diagrams vs figures):
		- "diagram" blocks are SVG/Mermaid and are best for precise, labeled, math-y visuals (flows, free-body diagrams, graphs).
		- SVG diagrams may include simple <animate>/<animateTransform> to illustrate motion or step transitions (no scripts; keep it subtle).
		- If diagram.kind="mermaid": diagram.source MUST be ONLY the Mermaid spec (no prose, no "Diagram" label, no backticks/code fences). Put explanation in diagram.caption.
		- If diagram.kind="svg": diagram.source MUST be ONLY a standalone <svg>…</svg> string (no prose). Put explanation in diagram.caption.
		- "figure" blocks are raster images and are best for higher-fidelity intuition, setups, and real-world context (“vibes”) where diagrams fall short.
	- Do NOT put labels/text inside figures; keep labels in captions and use diagrams for labeled visuals.

	Schema contract:
	- Use "order" as the only render order. Each order item is {kind,id}.
	- Each order item must reference exactly one object with the same id in the corresponding array:
	  headings | paragraphs | callouts | codes | figures | videos | diagrams | tables | objectives | prerequisites | key_takeaways | glossary | common_mistakes | misconceptions | edge_cases | heuristics | steps | checklist | faq | intuition | mental_model | why_it_matters | connections | quick_checks | dividers.
	- IDs must be non-empty and unique within each array (e.g., "h1","p2","qc3").
	- Do not create orphan blocks: every object you create must appear in "order".

	Block type conventions (use when helpful; leave arrays empty when not needed):
	- objectives/prerequisites/key_takeaways/common_mistakes/misconceptions/edge_cases/heuristics/connections:
	  - Use items_md: each entry is one bullet line (no leading "-" and no numbering).
	- steps:
	  - Use steps_md: each entry is one step (no leading "1."/"Step 1").
	- checklist:
	  - Use items_md: each entry is one checklist item (no leading "- [ ]").
	- glossary:
	  - Use terms: {term, definition_md}. Definitions are short and operational (not textbooky).
		- faq:
		  - Use qas: {question_md, answer_md}. Keep answers tight; no rambling.
		- intuition/mental_model/why_it_matters:
		  - Use md: a short, vivid section that adds insight (not generic filler).
		  - For intuition and mental_model blocks: if a visualization would materially improve comprehension or memory, include a supporting visual block immediately before/after it (figure/video/diagram) and make the visual caption explicitly reinforce that specific intuition/mental model.

		Hard rules:
		- Output ONLY valid JSON that matches the schema. No surrounding text.
		- Do not include planning/meta/check-in language. Do not offer customization options.
	- Quick checks must be short-answer or true/false (no multiple choice).
	- Heading levels must be 2, 3, or 4 (never use 1).
	- Include a tip callout titled exactly "Worked example".
	%s
	- If MUST_CITE_CHUNK_IDS is provided, each listed chunk_id must appear at least once in citations.
	- Every content block (everything except heading/divider/video/code) MUST include non-empty citations.
	- Citations MUST reference ONLY the provided chunk_ids.
	- Each citation is {chunk_id, quote (short), loc:{page,start,end}}. Use 0 for unknown locs.
	- Use markdown in md fields; do not include raw HTML.
	- If using a figure/video URL, it MUST come from AVAILABLE_MEDIA_ASSETS_JSON.
	%s`, diagramRuleLine, diagramPrefRule)

			var lastErrors []string
			for attempt := 1; attempt <= 3; attempt++ {
				start := time.Now()

				feedback := ""
				if len(lastErrors) > 0 {
					feedback = "\n\nVALIDATION_ERRORS_TO_FIX:\n- " + strings.Join(lastErrors, "\n- ")
				}

				generatedFigures := ""
				if hasGeneratedFigures {
					var b strings.Builder
					b.WriteString("\nGENERATED_FIGURE_ASSETS (use these URLs for figure blocks; do not use them as diagrams):\n")
					for _, a := range figAssetsByNode[w.Node.ID] {
						if a == nil || strings.TrimSpace(a.URL) == "" {
							continue
						}
						b.WriteString("- url=")
						b.WriteString(strings.TrimSpace(a.URL))
						if strings.TrimSpace(a.Notes) != "" {
							b.WriteString(" notes=")
							b.WriteString(strings.TrimSpace(a.Notes))
						}
						if len(a.ChunkIDs) > 0 {
							cids := make([]string, 0, len(a.ChunkIDs))
							for _, s := range dedupeStrings(a.ChunkIDs) {
								s = strings.TrimSpace(s)
								if s == "" {
									continue
								}
								id, err := uuid.Parse(s)
								if err != nil || id == uuid.Nil {
									continue
								}
								s = id.String()
								if allowedChunkIDs != nil && len(allowedChunkIDs) > 0 && !allowedChunkIDs[s] {
									continue
								}
								cids = append(cids, s)
							}
							if len(cids) > 0 {
								b.WriteString(" chunk_ids=")
								b.WriteString(strings.Join(cids, ","))
							}
						}
						b.WriteString("\n")
					}
					generatedFigures = strings.TrimSpace(b.String())
				}

				intentForPrompt := strings.TrimSpace(pathIntentMD)
				if intentForPrompt == "" {
					intentForPrompt = "(none)"
				}
				suggestedOutline := docTemplateSuggestedOutline(w.NodeKind, w.DocTemplate)

				teachingJSON, _ := teachingPatternsJSON(gctx, deps.Vec, deps.TeachingPatterns, w.QueryEmb, 4)
				if strings.TrimSpace(teachingJSON) == "" {
					teachingJSON = "(none)"
				}

				user := fmt.Sprintf(`
NODE_TITLE: %s
NODE_GOAL: %s
CONCEPT_KEYS: %s
NODE_KIND: %s
DOC_TEMPLATE: %s

USER_PROFILE_DOC (learner personalization; do not mention explicitly):
%s

TEACHING_PATTERNS_JSON (optional; pick 1-2 patterns and apply them without naming pattern keys):
%s

PATH_INTENT_MD (optional; global goal context):
%s

MUST_CITE_CHUNK_IDS (each must appear at least once in citations; if empty, ignore):
%s

REQUIREMENTS:
- Minimum word_count: %d
- Minimum paragraph blocks: %d
- Minimum callout blocks: %d
- Minimum quick_check blocks: %d
- Minimum headings (level 2-4): %d
- Minimum diagram blocks: %d
- Minimum table blocks: %d
- Minimum why_it_matters blocks: %d
- Minimum intuition blocks: %d
- Minimum mental_model blocks: %d
- Minimum misconceptions/common_mistakes blocks: %d
- Minimum steps blocks: %d
- Minimum checklist blocks: %d
- Must include a tip callout titled exactly "Worked example"
%s
%s
%s
%s

PATH_STYLE_JSON (optional; style only, no warnings):
%s

SUGGESTED_SECTION_OUTLINE (internal guidance; learner-facing doc should NOT mention this as an outline):
%s
%s
%s

GROUNDING_EXCERPTS (chunk_id lines):
%s

ALLOWED_CITATION_CHUNK_IDS (use ONLY these chunk_id values; do not invent new ones):
%s

AVAILABLE_MEDIA_ASSETS_JSON (optional; ONLY use listed URLs):
%s
%s

Output:
Return ONLY JSON matching schema.`, w.Node.Title, w.Goal, w.ConceptCSV, w.NodeKind, w.DocTemplate, userProfileDoc, teachingJSON, intentForPrompt, formatChunkIDBullets(mustCiteIDs), reqs.MinWordCount, reqs.MinParagraphs, reqs.MinCallouts, reqs.MinQuickChecks, reqs.MinHeadings, reqs.MinDiagrams, reqs.MinTables, reqs.MinWhyItMatters, reqs.MinIntuition, reqs.MinMentalModels, reqs.MinPitfalls, reqs.MinSteps, reqs.MinChecklist, mediaRequirementLine, generatedFigureRequirementLine, citationsRequirementLine, templateRequirementLine, pathStyleJSON, suggestedOutline, outlineDiagramLine, suggestedVideoLine(videoAsset), excerpts, formatChunkIDBullets(chunkIDs), assetsJSON, generatedFigures) + feedback

				obj, genErr := deps.AI.GenerateJSON(gctx, system, user, "node_doc_gen_v1", docSchema)
				latency := int(time.Since(start).Milliseconds())

				if genErr != nil {
					// Misconfigured schema is a deterministic 400; retrying wastes time.
					if strings.Contains(strings.ToLower(genErr.Error()), "invalid_json_schema") {
						return fmt.Errorf("node_doc_build: openai schema rejected: %w", genErr)
					}
					lastErrors = []string{"generate_failed: " + genErr.Error()}
					if deps.GenRuns != nil {
						_, _ = deps.GenRuns.Create(dbctx.Context{Ctx: ctx}, []*types.LearningDocGenerationRun{
							makeGenRun("node_doc", nil, in.OwnerUserID, pathID, w.Node.ID, "failed", nodeDocPromptVersion, attempt, latency, lastErrors, nil),
						})
					}
					// Retry bounded attempts (OpenAI client already retries transient HTTP failures).
					continue
				}

				var gen content.NodeDocGenV1
				rawDoc, _ := json.Marshal(obj)
				if uErr := json.Unmarshal(rawDoc, &gen); uErr != nil {
					lastErrors = []string{"schema_unmarshal_failed"}
					if deps.GenRuns != nil {
						_, _ = deps.GenRuns.Create(dbctx.Context{Ctx: ctx}, []*types.LearningDocGenerationRun{
							makeGenRun("node_doc", nil, in.OwnerUserID, pathID, w.Node.ID, "failed", nodeDocPromptVersion, attempt, latency, lastErrors, nil),
						})
					}
					continue
				}

				doc, convErrs := content.ConvertNodeDocGenV1ToV1(gen)
				if len(convErrs) > 0 {
					lastErrors = append([]string{"convert_failed"}, convErrs...)
					if deps.GenRuns != nil {
						_, _ = deps.GenRuns.Create(dbctx.Context{Ctx: ctx}, []*types.LearningDocGenerationRun{
							makeGenRun("node_doc", nil, in.OwnerUserID, pathID, w.Node.ID, "failed", nodeDocPromptVersion, attempt, latency, lastErrors, nil),
						})
					}
					continue
				}

				// Best-effort pruning of onboarding/meta blocks that should never appear in static docs.
				if pruned, hit := content.PruneNodeDocMetaBlocks(doc); len(hit) > 0 {
					doc = pruned
				}

				// Deterministic scrub pass to remove banned meta phrasing that occasionally slips through.
				// (We still validate after scrubbing; if it still fails, we retry generation.)
				if scrubbed, phrases := content.ScrubNodeDocV1(doc); len(phrases) > 0 {
					doc = scrubbed
				}

				// Best-effort: remove duplicated blocks (models sometimes repeat identical lines/paragraphs).
				if deduped, hit := content.DedupNodeDocV1(doc); len(hit) > 0 {
					doc = deduped
				}

				// Best-effort auto-injection to avoid wasting retries on simple omissions.
				if requireDiagrams {
					doc = ensureNodeDocHasDiagram(doc, allowedChunkIDs, chunkIDs)
				}
				if hasGeneratedFigures && shouldAutoInjectGeneratedFigure(reqs) {
					doc = ensureNodeDocHasGeneratedFigure(doc, figAssetsByNode[w.Node.ID], allowedChunkIDs, chunkIDs)
				}
				if diagramsDisabled {
					doc = removeNodeDocBlockType(doc, "diagram")
				} else if diagramLimit > 0 {
					doc = capNodeDocBlockType(doc, "diagram", diagramLimit)
				}
				// Dedupe again after any auto-injection/capping to keep the final doc clean.
				if deduped, hit := content.DedupNodeDocV1(doc); len(hit) > 0 {
					doc = deduped
				}
				if withIDs, changed := content.EnsureNodeDocBlockIDs(doc); changed {
					doc = withIDs
				}

				if patched, changed := sanitizeNodeDocDiagrams(doc); changed {
					doc = patched
				}

				var citationStats citationSanitizeStats
				citationsSanitized := false
				if patched, stats, changed := sanitizeNodeDocCitations(doc, allowedChunkIDs, chunkByID, chunkIDs); changed {
					doc = patched
					citationStats = stats
					citationsSanitized = true
				}

				errs, metrics := content.ValidateNodeDocV1(doc, allowedChunkIDs, reqs)
				if citationsSanitized {
					metrics["citations_sanitized"] = true
					metrics["citations_sanitize"] = citationStats.Map()
				}
				// Coverage enforcement: ensure assigned must-cite chunk IDs actually appear in citations.
				if len(mustCiteIDsAllowed) > 0 {
					missing := missingMustCiteIDs(doc, mustCiteIDsAllowed)
					if len(missing) > 0 {
						if patched, ok := injectMissingMustCiteCitations(doc, missing, chunkByID); ok {
							doc = patched
							errs, metrics = content.ValidateNodeDocV1(doc, allowedChunkIDs, reqs)
							missing = missingMustCiteIDs(doc, mustCiteIDsAllowed)
							if len(missing) == 0 {
								metrics["must_cite_injected"] = true
							}
						}
					}
					if len(missing) > 0 {
						metrics["must_cite_missing"] = missing
						errs = append(errs, "missing required citations for chunk_ids: "+strings.Join(missing, ", "))
					}
				}
				if len(errs) > 0 {
					lastErrors = errs
					if deps.GenRuns != nil {
						_, _ = deps.GenRuns.Create(dbctx.Context{Ctx: ctx}, []*types.LearningDocGenerationRun{
							makeGenRun("node_doc", nil, in.OwnerUserID, pathID, w.Node.ID, "failed", nodeDocPromptVersion, attempt, latency, errs, metrics),
						})
					}
					continue
				}

				// Persist the scrubbed-and-validated doc (not the raw model output bytes).
				rawDoc, _ = json.Marshal(doc)
				canon, cErr := content.CanonicalizeJSON(rawDoc)
				if cErr != nil {
					return cErr
				}
				contentHash := content.HashBytes(canon)
				sourcesHash := content.HashSources(nodeDocPromptVersion, 1, mapKeys(allowedChunkIDs))

				docText, _ := metrics["doc_text"].(string)
				docText = content.SanitizeStringForPostgres(docText)

				docID := uuid.New()
				row := &types.LearningNodeDoc{
					ID:            docID,
					UserID:        in.OwnerUserID,
					PathID:        pathID,
					PathNodeID:    w.Node.ID,
					SchemaVersion: 1,
					DocJSON:       datatypes.JSON(canon),
					DocText:       docText,
					ContentHash:   contentHash,
					SourcesHash:   sourcesHash,
					CreatedAt:     time.Now().UTC(),
					UpdatedAt:     time.Now().UTC(),
				}
				if err := deps.NodeDocs.Upsert(dbctx.Context{Ctx: ctx}, row); err != nil {
					return err
				}

				if deps.GenRuns != nil {
					_, _ = deps.GenRuns.Create(dbctx.Context{Ctx: ctx}, []*types.LearningDocGenerationRun{
						makeGenRun("node_doc", &docID, in.OwnerUserID, pathID, w.Node.ID, "succeeded", nodeDocPromptVersion, attempt, latency, nil, metrics),
					})
				}

				if bcAny, ok := metrics["block_counts"]; ok && bcAny != nil {
					if bc, ok := bcAny.(map[string]int); ok {
						atomic.AddInt32(&diagrams, int32(bc["diagram"]))
						atomic.AddInt32(&figures, int32(bc["figure"]))
						atomic.AddInt32(&videos, int32(bc["video"]))
						atomic.AddInt32(&tables, int32(bc["table"]))
					}
				}
				atomic.AddInt32(&written, 1)
				return nil
			}

			return fmt.Errorf("node_doc_build: failed validation after retries (path_node_id=%s errors=%v)", w.Node.ID.String(), lastErrors)
		})
	}

	if err := g.Wait(); err != nil {
		return out, err
	}

	out.DocsWritten = int(atomic.LoadInt32(&written))
	out.DiagramsWritten = int(atomic.LoadInt32(&diagrams))
	out.FiguresWritten = int(atomic.LoadInt32(&figures))
	out.VideosWritten = int(atomic.LoadInt32(&videos))
	out.TablesWritten = int(atomic.LoadInt32(&tables))

	return out, nil
}
