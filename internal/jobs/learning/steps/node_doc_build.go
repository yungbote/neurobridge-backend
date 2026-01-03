package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/clients/gcp"
	"github.com/yungbote/neurobridge-backend/internal/clients/openai"
	pc "github.com/yungbote/neurobridge-backend/internal/clients/pinecone"
	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/learning/content"
	"github.com/yungbote/neurobridge-backend/internal/learning/content/schema"
	"github.com/yungbote/neurobridge-backend/internal/learning/index"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
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

const nodeDocPromptVersion = "node_doc_v1@2"

func NodeDocBuild(ctx context.Context, deps NodeDocBuildDeps, in NodeDocBuildInput) (NodeDocBuildOutput, error) {
	out := NodeDocBuildOutput{}
	if deps.DB == nil || deps.Log == nil || deps.Path == nil || deps.PathNodes == nil || deps.NodeDocs == nil || deps.Files == nil || deps.Chunks == nil || deps.AI == nil || deps.Bootstrap == nil {
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

	pathRow, err := deps.Path.GetByID(dbctx.Context{Ctx: ctx}, pathID)
	if err != nil {
		return out, err
	}
	pathStyleJSON := ""
	if pathRow != nil && len(pathRow.Metadata) > 0 && string(pathRow.Metadata) != "null" {
		var meta map[string]any
		if json.Unmarshal(pathRow.Metadata, &meta) == nil {
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
		Node       *types.PathNode
		Goal       string
		ConceptCSV string
		QueryText  string
		QueryEmb   []float32
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
		nodeGoal := strings.TrimSpace(stringFromAny(nodeMeta["goal"]))
		nodeConceptKeys := dedupeStrings(stringSliceFromAny(nodeMeta["concept_keys"]))
		conceptCSV := strings.Join(nodeConceptKeys, ", ")

		queryText := strings.TrimSpace(node.Title + " " + nodeGoal + " " + conceptCSV)

		work = append(work, nodeWork{
			Node:       node,
			Goal:       nodeGoal,
			ConceptCSV: conceptCSV,
			QueryText:  queryText,
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
				ids, qerr := deps.Vec.QueryIDs(gctx, chunksNS, w.QueryEmb, semanticK, map[string]any{"type": "chunk"})
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

			// ---- Learner-facing doc with validation + retry ----
			reqs := content.DefaultNodeDocRequirements()
			diagramLimit := envIntAllowZero("NODE_DOC_DIAGRAMS_LIMIT", -1)
			if diagramLimit < 0 {
				diagramLimit = -1
			}
			diagramsDisabled := diagramLimit == 0
			if diagramsDisabled {
				reqs.MinDiagrams = 0
			}
			requireGeneratedFigure := len(figAssetsByNode[w.Node.ID]) > 0
			// Best-effort: if a video is available in the allowed assets list, include it (we can auto-inject on fallback).
			videoAsset := firstVideoAssetFromAssetsJSON(assetsJSON)

			mediaRequirementLine := "- Must include at least one of: figure | diagram | table"
			generatedFigureRequirementLine := `- If GENERATED_FIGURE_ASSETS is non-empty, include at least 1 figure block using one of those URLs (in addition to any diagrams/tables you include).`
			citationsRequirementLine := "- Every paragraph/callout/figure/diagram/table/quick_check must have citations (non-empty)"
			outlineDiagramLine := "- Include at least one diagram early."
			diagramHardRule := "- Include at least one diagram block (SVG preferred)."
			diagramPrefRule := `- Prefer diagram.kind="svg" (simple, readable SVG).`
			if diagramsDisabled {
				mediaRequirementLine = "- Must include at least one of: figure | table"
				generatedFigureRequirementLine = `- If GENERATED_FIGURE_ASSETS is non-empty, include at least 1 figure block using one of those URLs (in addition to any tables you include).`
				citationsRequirementLine = "- Every paragraph/callout/figure/table/quick_check must have citations (non-empty)"
				outlineDiagramLine = "- Do not include any diagram blocks."
				diagramHardRule = "- Do not include any diagram blocks."
				diagramPrefRule = ""
			}

			system := fmt.Sprintf(`
MODE: STATIC_UNIT_DOC

You write dense, structured learning unit docs in a "docs page" style.
This is NOT an interactive chat: do not ask the learner any questions or solicit preferences.
Do not include any onboarding sections ("Entry Check", "Your goal/level", "Format preferences", etc).
The only questions in the entire doc must be inside quick_check blocks.

Media rules (diagrams vs figures):
- "diagram" blocks are SVG/Mermaid and are best for precise, labeled, math-y visuals (flows, free-body diagrams, graphs).
- SVG diagrams may include simple <animate>/<animateTransform> to illustrate motion or step transitions (no scripts; keep it subtle).
- "figure" blocks are raster images and are best for higher-fidelity intuition, setups, and real-world context (“vibes”) where diagrams fall short.
- Do NOT put labels/text inside figures; keep labels in captions and use diagrams for labeled visuals.

Schema contract:
- Use "order" as the only render order. Each order item is {kind,id}.
- Each order item must reference exactly one object with the same id in the corresponding array:
  headings | paragraphs | callouts | codes | figures | videos | diagrams | tables | quick_checks | dividers.
- IDs must be non-empty and unique within each array (e.g., "h1","p2","qc3").
- Do not create orphan blocks: every object you create must appear in "order".

Hard rules:
- Output ONLY valid JSON that matches the schema. No surrounding text.
- Do not include planning/meta/check-in language. Do not offer customization options.
- Quick checks must be short-answer or true/false (no multiple choice).
- Heading levels must be 2, 3, or 4 (never use 1).
- Include a tip callout titled exactly "Worked example".
%s
- If MUST_CITE_CHUNK_IDS is provided, each listed chunk_id must appear at least once in citations.
- Every paragraph/callout/figure/diagram/table/quick_check MUST include non-empty citations.
- Citations MUST reference ONLY the provided chunk_ids.
- Each citation is {chunk_id, quote (short), loc:{page,start,end}}. Use 0 for unknown locs.
- Use markdown in md fields; do not include raw HTML.
- If using a figure/video URL, it MUST come from AVAILABLE_MEDIA_ASSETS_JSON.
%s`, diagramHardRule, diagramPrefRule)

			var lastErrors []string
			for attempt := 1; attempt <= 3; attempt++ {
				start := time.Now()

				feedback := ""
				if len(lastErrors) > 0 {
					feedback = "\n\nVALIDATION_ERRORS_TO_FIX:\n- " + strings.Join(lastErrors, "\n- ")
				}

				generatedFigures := ""
				if requireGeneratedFigure {
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
							b.WriteString(" chunk_ids=")
							b.WriteString(strings.Join(dedupeStrings(a.ChunkIDs), ","))
						}
						b.WriteString("\n")
					}
					generatedFigures = strings.TrimSpace(b.String())
				}

				user := fmt.Sprintf(`
NODE_TITLE: %s
NODE_GOAL: %s
CONCEPT_KEYS: %s

MUST_CITE_CHUNK_IDS (each must appear at least once in citations; if empty, ignore):
%s

REQUIREMENTS:
- Minimum word_count: %d
- Minimum quick_check blocks: %d
- Minimum headings (level 2-4): %d
- Minimum diagram blocks: %d
- Must include a tip callout titled exactly "Worked example"
%s
%s
%s

PATH_STYLE_JSON (optional; style only, no warnings):
%s

SUGGESTED_SECTION_OUTLINE (internal guidance; learner-facing doc should NOT mention this as an outline):
- Start with a level-2 heading that frames the core idea in 1 sentence.
- Add a level-2 heading that defines key terms and connects them to the goal.
- Add a level-2 heading that explains the main mechanism/logic step-by-step.
- Include a "Worked example" as a tip callout (title exactly "Worked example").
- End with a level-2 heading that lists common misconceptions + corrections.
- Spread >= %d quick checks throughout (not all at the end).
%s
%s

GROUNDING_EXCERPTS (chunk_id lines):
%s

AVAILABLE_MEDIA_ASSETS_JSON (optional; ONLY use listed URLs):
%s
%s

Output:
Return ONLY JSON matching schema.`, w.Node.Title, w.Goal, w.ConceptCSV, formatChunkIDBullets(mustCiteIDs), reqs.MinWordCount, reqs.MinQuickChecks, reqs.MinHeadings, reqs.MinDiagrams, mediaRequirementLine, generatedFigureRequirementLine, citationsRequirementLine, pathStyleJSON, reqs.MinQuickChecks, outlineDiagramLine, suggestedVideoLine(videoAsset), excerpts, assetsJSON, generatedFigures) + feedback

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

				// Best-effort auto-injection to avoid wasting retries on simple omissions.
				if !diagramsDisabled {
					doc = ensureNodeDocHasDiagram(doc, allowedChunkIDs, chunkIDs)
				}
				if requireGeneratedFigure {
					doc = ensureNodeDocHasGeneratedFigure(doc, figAssetsByNode[w.Node.ID], allowedChunkIDs, chunkIDs)
				}
				if videoAsset != nil {
					doc = ensureNodeDocHasVideo(doc, videoAsset)
				}
				if diagramsDisabled {
					doc = removeNodeDocBlockType(doc, "diagram")
				} else if diagramLimit > 0 {
					doc = capNodeDocBlockType(doc, "diagram", diagramLimit)
				}
				if withIDs, changed := content.EnsureNodeDocBlockIDs(doc); changed {
					doc = withIDs
				}

				errs, metrics := content.ValidateNodeDocV1(doc, allowedChunkIDs, reqs)
				// Coverage enforcement: ensure assigned must-cite chunk IDs actually appear in citations.
				if len(mustCiteIDs) > 0 {
					missing := missingMustCiteIDs(doc, mustCiteIDs)
					if len(missing) > 0 {
						if patched, ok := injectMissingMustCiteCitations(doc, missing, chunkByID); ok {
							doc = patched
							errs, metrics = content.ValidateNodeDocV1(doc, allowedChunkIDs, reqs)
							missing = missingMustCiteIDs(doc, mustCiteIDs)
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
				if requireGeneratedFigure {
					figCount := 0
					if bcAny, ok := metrics["block_counts"]; ok && bcAny != nil {
						if bc, ok := bcAny.(map[string]int); ok {
							figCount = bc["figure"]
						}
					}
					if figCount == 0 {
						// Fallback in case block_counts type assertion failed.
						if figCount == 0 {
							if bc, ok := content.NodeDocMetrics(doc)["block_counts"].(map[string]int); ok {
								figCount = bc["figure"]
							}
						}
					}
					if figCount == 0 {
						errs = append(errs, "need at least one figure block (generated figure assets are available)")
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

func mergeUUIDListsPreserveOrder(lists ...[]uuid.UUID) []uuid.UUID {
	seen := map[uuid.UUID]bool{}
	out := make([]uuid.UUID, 0)
	for _, l := range lists {
		for _, id := range l {
			if id == uuid.Nil || seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

func formatChunkIDBullets(ids []uuid.UUID) string {
	if len(ids) == 0 {
		return ""
	}
	var b strings.Builder
	for _, id := range ids {
		if id == uuid.Nil {
			continue
		}
		b.WriteString("- ")
		b.WriteString(id.String())
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func firstVideoAssetFromAssetsJSON(assetsJSON string) *mediaAssetCandidate {
	s := strings.TrimSpace(assetsJSON)
	if s == "" {
		return nil
	}
	var payload struct {
		Assets []*mediaAssetCandidate `json:"assets"`
	}
	if err := json.Unmarshal([]byte(s), &payload); err != nil {
		return nil
	}
	// Prefer generated videos when present so unit docs use Sora outputs by default.
	for _, a := range payload.Assets {
		if a == nil {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(a.Kind), "video") && strings.EqualFold(strings.TrimSpace(a.AssetKind), "generated_video") && strings.TrimSpace(a.URL) != "" {
			return a
		}
	}
	for _, a := range payload.Assets {
		if a == nil {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(a.Kind), "video") && strings.TrimSpace(a.URL) != "" {
			return a
		}
	}
	return nil
}

func suggestedVideoLine(videoAsset *mediaAssetCandidate) string {
	if videoAsset == nil {
		return ""
	}
	return "- If a relevant video is available in AVAILABLE_MEDIA_ASSETS_JSON, include 1 short video block and caption what to watch for."
}

func removeNodeDocBlockType(doc content.NodeDocV1, blockType string) content.NodeDocV1 {
	blockType = strings.TrimSpace(blockType)
	if blockType == "" || len(doc.Blocks) == 0 {
		return doc
	}
	out := make([]map[string]any, 0, len(doc.Blocks))
	for _, b := range doc.Blocks {
		if strings.EqualFold(strings.TrimSpace(stringFromAny(b["type"])), blockType) {
			continue
		}
		out = append(out, b)
	}
	doc.Blocks = out
	return doc
}

func capNodeDocBlockType(doc content.NodeDocV1, blockType string, max int) content.NodeDocV1 {
	if max < 0 {
		return doc
	}
	if max == 0 {
		return removeNodeDocBlockType(doc, blockType)
	}
	blockType = strings.TrimSpace(blockType)
	if blockType == "" || len(doc.Blocks) == 0 {
		return doc
	}
	kept := 0
	out := make([]map[string]any, 0, len(doc.Blocks))
	for _, b := range doc.Blocks {
		if strings.EqualFold(strings.TrimSpace(stringFromAny(b["type"])), blockType) {
			kept++
			if kept > max {
				continue
			}
		}
		out = append(out, b)
	}
	doc.Blocks = out
	return doc
}

func ensureNodeDocHasDiagram(doc content.NodeDocV1, allowedChunkIDs map[string]bool, fallbackChunkIDs []uuid.UUID) content.NodeDocV1 {
	for _, b := range doc.Blocks {
		if strings.EqualFold(strings.TrimSpace(stringFromAny(b["type"])), "diagram") {
			return doc
		}
	}
	cid := ""
	for _, id := range fallbackChunkIDs {
		if id == uuid.Nil {
			continue
		}
		s := id.String()
		if allowedChunkIDs != nil && len(allowedChunkIDs) > 0 && !allowedChunkIDs[s] {
			continue
		}
		cid = s
		break
	}
	if cid == "" {
		return doc
	}

	labels := make([]string, 0, 4)
	for _, k := range doc.ConceptKeys {
		k = strings.TrimSpace(strings.ReplaceAll(k, "_", " "))
		if k == "" {
			continue
		}
		labels = append(labels, k)
		if len(labels) >= 4 {
			break
		}
	}
	if len(labels) == 0 {
		if strings.TrimSpace(doc.Title) != "" {
			labels = append(labels, strings.TrimSpace(doc.Title))
		} else {
			labels = append(labels, "Core idea")
		}
	}

	for i := range labels {
		labels[i] = shorten(labels[i], 28)
	}

	svg := buildSimpleFlowSVG(labels)
	if strings.TrimSpace(svg) == "" {
		return doc
	}

	blockID := "auto_diagram_" + uuid.New().String()
	block := map[string]any{
		"id":      blockID,
		"type":    "diagram",
		"kind":    "svg",
		"source":  svg,
		"caption": "Concept relationship overview",
		"citations": []any{
			map[string]any{
				"chunk_id": cid,
				"quote":    "",
				"loc":      map[string]any{"page": 0, "start": 0, "end": 0},
			},
		},
	}
	doc.Blocks = insertAfterFirstBodyBlock(doc.Blocks, block)
	return doc
}

func ensureNodeDocHasGeneratedFigure(doc content.NodeDocV1, figs []*mediaAssetCandidate, allowedChunkIDs map[string]bool, fallbackChunkIDs []uuid.UUID) content.NodeDocV1 {
	has := false
	for _, b := range doc.Blocks {
		if strings.EqualFold(strings.TrimSpace(stringFromAny(b["type"])), "figure") {
			has = true
			break
		}
	}
	if has {
		return doc
	}
	var a *mediaAssetCandidate
	for _, it := range figs {
		if it != nil && strings.TrimSpace(it.URL) != "" {
			a = it
			break
		}
	}
	if a == nil {
		return doc
	}

	pickCitationID := func() string {
		for _, s := range a.ChunkIDs {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			if allowedChunkIDs != nil && len(allowedChunkIDs) > 0 && !allowedChunkIDs[s] {
				continue
			}
			if _, err := uuid.Parse(s); err != nil {
				continue
			}
			return s
		}
		for _, id := range fallbackChunkIDs {
			if id == uuid.Nil {
				continue
			}
			s := id.String()
			if allowedChunkIDs != nil && len(allowedChunkIDs) > 0 && !allowedChunkIDs[s] {
				continue
			}
			return s
		}
		return ""
	}

	cid := pickCitationID()
	if cid == "" {
		return doc
	}

	caption := strings.TrimSpace(extractNoteValue(a.Notes, "caption="))
	if caption == "" {
		caption = "Supplementary figure (generated from your materials)"
	}
	source := strings.TrimSpace(a.Source)
	if source == "" {
		source = "derived"
	}
	fileName := strings.TrimSpace(a.FileName)
	if fileName == "" {
		fileName = "figure.png"
	}
	mime := strings.TrimSpace(a.MimeType)
	if mime == "" {
		mime = "image/png"
	}

	blockID := "auto_figure_" + uuid.New().String()
	block := map[string]any{
		"id":   blockID,
		"type": "figure",
		"asset": map[string]any{
			"url":              strings.TrimSpace(a.URL),
			"material_file_id": "",
			"storage_key":      strings.TrimSpace(a.Key),
			"mime_type":        mime,
			"file_name":        fileName,
			"source":           source,
		},
		"caption": caption,
		"citations": []any{
			map[string]any{
				"chunk_id": cid,
				"quote":    "",
				"loc":      map[string]any{"page": 0, "start": 0, "end": 0},
			},
		},
	}

	doc.Blocks = insertAfterFirstBodyBlock(doc.Blocks, block)
	return doc
}

func ensureNodeDocHasVideo(doc content.NodeDocV1, videoAsset *mediaAssetCandidate) content.NodeDocV1 {
	if videoAsset == nil || strings.TrimSpace(videoAsset.URL) == "" {
		return doc
	}
	for _, b := range doc.Blocks {
		if strings.EqualFold(strings.TrimSpace(stringFromAny(b["type"])), "video") {
			return doc
		}
	}
	startSec := 0.0
	if videoAsset.StartSec != nil && *videoAsset.StartSec > 0 {
		startSec = *videoAsset.StartSec
	}
	caption := strings.TrimSpace(extractNoteValue(videoAsset.Notes, "caption="))
	if caption == "" {
		caption = strings.TrimSpace(videoAsset.FileName)
	}
	if caption == "" {
		caption = strings.TrimSpace(videoAsset.Notes)
		if caption == "" {
			caption = "Supplementary video (from your materials)"
		}
	}
	blockID := "auto_video_" + uuid.New().String()
	block := map[string]any{
		"id":        blockID,
		"type":      "video",
		"url":       strings.TrimSpace(videoAsset.URL),
		"start_sec": startSec,
		"caption":   shorten(caption, 140),
	}
	doc.Blocks = insertAfterFirstBodyBlock(doc.Blocks, block)
	return doc
}

func extractNoteValue(notes string, prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if strings.TrimSpace(notes) == "" || prefix == "" {
		return ""
	}
	for _, part := range strings.Split(notes, "|") {
		p := strings.TrimSpace(part)
		if strings.HasPrefix(p, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(p, prefix))
		}
	}
	return ""
}

func insertAfterFirstBodyBlock(blocks []map[string]any, block map[string]any) []map[string]any {
	if block == nil {
		return blocks
	}
	if len(blocks) == 0 {
		return []map[string]any{block}
	}
	insertAt := len(blocks)
	for i := range blocks {
		t := strings.ToLower(strings.TrimSpace(stringFromAny(blocks[i]["type"])))
		if t == "paragraph" || t == "callout" || t == "diagram" || t == "table" || t == "code" {
			insertAt = i + 1
			break
		}
	}
	out := make([]map[string]any, 0, len(blocks)+1)
	out = append(out, blocks[:insertAt]...)
	out = append(out, block)
	out = append(out, blocks[insertAt:]...)
	return out
}

func missingMustCiteIDs(doc content.NodeDocV1, mustCiteIDs []uuid.UUID) []string {
	if len(mustCiteIDs) == 0 {
		return nil
	}
	cited := map[string]bool{}
	for _, s := range content.CitedChunkIDsFromNodeDocV1(doc) {
		cited[strings.TrimSpace(s)] = true
	}
	missing := make([]string, 0)
	for _, id := range mustCiteIDs {
		if id == uuid.Nil {
			continue
		}
		s := id.String()
		if !cited[s] {
			missing = append(missing, s)
		}
	}
	return missing
}

func injectMissingMustCiteCitations(doc content.NodeDocV1, missing []string, chunkByID map[uuid.UUID]*types.MaterialChunk) (content.NodeDocV1, bool) {
	if len(missing) == 0 {
		return doc, false
	}
	idx := firstCitationBlockIndex(doc.Blocks)
	if idx < 0 || idx >= len(doc.Blocks) {
		return doc, false
	}
	block := doc.Blocks[idx]
	citations := make([]any, 0)
	if existing, ok := block["citations"].([]any); ok {
		citations = append(citations, existing...)
	}
	for _, id := range missing {
		citations = append(citations, buildMustCiteRef(id, chunkByID))
	}
	block["citations"] = citations
	doc.Blocks[idx] = block
	return doc, true
}

func firstCitationBlockIndex(blocks []map[string]any) int {
	for i, b := range blocks {
		if b == nil {
			continue
		}
		t := strings.ToLower(strings.TrimSpace(stringFromAny(b["type"])))
		switch t {
		case "paragraph", "callout", "figure", "diagram", "table", "quick_check":
			return i
		}
	}
	return -1
}

func buildMustCiteRef(id string, chunkByID map[uuid.UUID]*types.MaterialChunk) map[string]any {
	quote := ""
	page := 0
	if parsed, err := uuid.Parse(strings.TrimSpace(id)); err == nil && parsed != uuid.Nil {
		if ch := chunkByID[parsed]; ch != nil {
			quote = shorten(strings.TrimSpace(ch.Text), 220)
			if ch.Page != nil {
				page = *ch.Page
			}
		}
	}
	return map[string]any{
		"chunk_id": strings.TrimSpace(id),
		"quote":    quote,
		"loc": map[string]any{
			"page":  page,
			"start": 0,
			"end":   0,
		},
	}
}

func buildSimpleFlowSVG(labels []string) string {
	labels = dedupeStrings(labels)
	if len(labels) == 0 {
		return ""
	}
	if len(labels) > 4 {
		labels = labels[:4]
	}

	const (
		w      = 900
		h      = 240
		margin = 24
		gap    = 22
		boxH   = 86
	)
	n := len(labels)
	innerW := w - margin*2 - gap*(n-1)
	if innerW < 120 {
		return ""
	}
	boxW := innerW / n
	y := (h - boxH) / 2

	var b strings.Builder
	b.WriteString(fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">`, w, h, w, h))
	b.WriteString(`
<style>
.box{fill:#f7f7fb;stroke:#2b2b2b;stroke-width:2;rx:14;}
.t{font-family:Arial, Helvetica, sans-serif;font-size:16px;fill:#111;}
.arrow{stroke:#111;stroke-width:2.5;marker-end:url(#m);}
</style>
<defs>
<marker id="m" markerWidth="10" markerHeight="10" refX="8" refY="3" orient="auto">
<path d="M0,0 L9,3 L0,6 Z" fill="#111"/>
</marker>
</defs>
`)

	for i, raw := range labels {
		x := margin + i*(boxW+gap)
		label := escapeXML(strings.TrimSpace(raw))
		b.WriteString(fmt.Sprintf(`<rect class="box" x="%d" y="%d" width="%d" height="%d"/>`, x, y, boxW, boxH))
		// Center text.
		tx := x + boxW/2
		ty := y + boxH/2 + 6
		b.WriteString(fmt.Sprintf(`<text class="t" x="%d" y="%d" text-anchor="middle">%s</text>`, tx, ty, label))

		// Arrow to next box.
		if i < n-1 {
			ax1 := x + boxW
			ay := y + boxH/2
			ax2 := x + boxW + gap - 6
			b.WriteString(fmt.Sprintf(`<line class="arrow" x1="%d" y1="%d" x2="%d" y2="%d"/>`, ax1, ay, ax2, ay))
		}
	}

	b.WriteString(`</svg>`)
	return b.String()
}

func escapeXML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	return s
}

func lexicalChunkIDs(dbc dbctx.Context, fileIDs []uuid.UUID, query string, limit int) ([]uuid.UUID, error) {
	transaction := dbc.Tx
	if transaction == nil || limit <= 0 || len(fileIDs) == 0 || strings.TrimSpace(query) == "" {
		return nil, nil
	}
	// Conservative: keep query short; plainto_tsquery struggles with huge input.
	query = shorten(query, 220)
	var ids []uuid.UUID
	err := transaction.WithContext(dbc.Ctx).Raw(`
		SELECT id
		FROM material_chunk
		WHERE deleted_at IS NULL
		  AND material_file_id IN ?
		  AND to_tsvector('english', text) @@ plainto_tsquery('english', ?)
		ORDER BY ts_rank_cd(to_tsvector('english', text), plainto_tsquery('english', ?)) DESC
		LIMIT ?
	`, fileIDs, query, query, limit).Scan(&ids).Error
	if err != nil {
		return nil, err
	}
	return dedupeUUIDsPreserveOrder(ids), nil
}

func dedupeUUIDsPreserveOrder(in []uuid.UUID) []uuid.UUID {
	seen := map[uuid.UUID]bool{}
	out := make([]uuid.UUID, 0, len(in))
	for _, id := range in {
		if id == uuid.Nil || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func mapKeys(m map[string]bool) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func makeGenRun(artifactType string, artifactID *uuid.UUID, userID, pathID, pathNodeID uuid.UUID, status, promptVersion string, attempt, latencyMS int, validationErrors []string, qualityMetrics map[string]any) *types.LearningDocGenerationRun {
	now := time.Now().UTC()
	ve := datatypes.JSON([]byte(`null`))
	if len(validationErrors) > 0 {
		b, _ := json.Marshal(validationErrors)
		ve = datatypes.JSON(b)
	}
	qm := datatypes.JSON([]byte(`null`))
	if qualityMetrics != nil {
		b, _ := json.Marshal(qualityMetrics)
		qm = datatypes.JSON(b)
	}
	model := strings.TrimSpace(openAIModelFromEnv())
	if model == "" {
		model = "unknown"
	}
	return &types.LearningDocGenerationRun{
		ID:               uuid.New(),
		ArtifactType:     artifactType,
		ArtifactID:       artifactID,
		UserID:           userID,
		PathID:           pathID,
		PathNodeID:       pathNodeID,
		Status:           status,
		Model:            model,
		PromptVersion:    promptVersion,
		Attempt:          attempt,
		LatencyMS:        latencyMS,
		TokensIn:         0,
		TokensOut:        0,
		ValidationErrors: ve,
		QualityMetrics:   qm,
		CreatedAt:        now,
	}
}

func openAIModelFromEnv() string {
	// Keep this local so we don't expand the openai.Client interface.
	return strings.TrimSpace(os.Getenv("OPENAI_MODEL"))
}
