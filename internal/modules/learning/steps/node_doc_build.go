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

	"github.com/yungbote/neurobridge-backend/internal/data/materialsetctx"
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

type nodeThreadSummary struct {
	Title       string
	Summary     string
	KeyTerms    []string
	ConceptKeys []string
}

type conceptBrief struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

type nodeNarrativeContext struct {
	PrevTitle    string   `json:"prev_title,omitempty"`
	PrevSummary  string   `json:"prev_summary,omitempty"`
	PrevKeyTerms []string `json:"prev_key_terms,omitempty"`

	NextTitle    string   `json:"next_title,omitempty"`
	NextSummary  string   `json:"next_summary,omitempty"`
	NextKeyTerms []string `json:"next_key_terms,omitempty"`

	ModuleTitle    string   `json:"module_title,omitempty"`
	ModuleGoal     string   `json:"module_goal,omitempty"`
	ModuleSiblings []string `json:"module_siblings,omitempty"`

	PrereqConcepts  []conceptBrief `json:"prereq_concepts,omitempty"`
	RelatedConcepts []conceptBrief `json:"related_concepts,omitempty"`
	AnalogyConcepts []conceptBrief `json:"analogy_concepts,omitempty"`
}

type sectionEvidence struct {
	Heading     string   `json:"heading"`
	Goal        string   `json:"goal"`
	ConceptKeys []string `json:"concept_keys,omitempty"`
	BridgeIn    string   `json:"bridge_in,omitempty"`
	BridgeOut   string   `json:"bridge_out,omitempty"`
	ChunkIDs    []string `json:"chunk_ids"`
	Excerpts    string   `json:"excerpts"`
}

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
	Concepts         repos.ConceptRepo
	ConceptState     repos.UserConceptStateRepo
	Edges            repos.ConceptEdgeRepo

	AI  openai.Client
	Vec pc.VectorStore

	Bucket gcp.BucketService

	Bootstrap services.LearningBuildBootstrapService
}

type NodeDocBuildInput struct {
	OwnerUserID   uuid.UUID
	MaterialSetID uuid.UUID
	SagaID        uuid.UUID
	PathID        uuid.UUID
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

const nodeDocPromptVersion = "node_doc_v2@1"

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

	pathID, err := resolvePathID(ctx, deps.Bootstrap, in.OwnerUserID, in.MaterialSetID, in.PathID)
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
	var patternHierarchy teachingPatternHierarchy
	pathMeta := map[string]any{}
	if pathRow != nil && len(pathRow.Metadata) > 0 && string(pathRow.Metadata) != "null" {
		if err := json.Unmarshal(pathRow.Metadata, &pathMeta); err != nil || pathMeta == nil {
			pathMeta = map[string]any{}
		}
	}
	if len(pathMeta) > 0 {
		if v, ok := pathMeta["intake_md"]; ok && v != nil {
			pathIntentMD = strings.TrimSpace(stringFromAny(v))
		}
		allowFiles = intakeMaterialAllowlistFromPathMeta(pathMeta)
		if v, ok := pathMeta["charter"]; ok && v != nil {
			// Extract just the stable style fields (avoid charter warnings like "ask 2-4 questions").
			if charter, ok := v.(map[string]any); ok {
				if ps, ok := charter["path_style"]; ok && ps != nil {
					if pb, err := json.Marshal(ps); err == nil {
						pathStyleJSON = string(pb)
					}
				}
			}
		}
		if v, ok := pathMeta["pattern_hierarchy"]; ok && v != nil {
			if pb, err := json.Marshal(v); err == nil {
				_ = json.Unmarshal(pb, &patternHierarchy)
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
	nodesByID := map[uuid.UUID]*types.PathNode{}
	for _, n := range nodes {
		if n != nil && n.ID != uuid.Nil {
			nodeIDs = append(nodeIDs, n.ID)
			nodesByID[n.ID] = n
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

	mediaUsedMu := sync.Mutex{}
	mediaUsed := map[string]bool{}
	for _, d := range existingDocs {
		if d == nil || len(d.DocJSON) == 0 || string(d.DocJSON) == "null" {
			continue
		}
		for _, url := range mediaURLsFromNodeDocJSON(d.DocJSON) {
			if url != "" {
				mediaUsed[url] = true
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

	type nodeInfo struct {
		Node        *types.PathNode
		NodeKind    string
		DocTemplate string
		Goal        string
		ConceptKeys []string
		PrereqKeys  []string
		ConceptCSV  string
		Meta        map[string]any
	}

	infoByID := map[uuid.UUID]nodeInfo{}
	for _, node := range nodes {
		if node == nil || node.ID == uuid.Nil {
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
		prereqKeys := dedupeStrings(stringSliceFromAny(nodeMeta["prereq_concept_keys"]))
		conceptCSV := strings.Join(nodeConceptKeys, ", ")

		infoByID[node.ID] = nodeInfo{
			Node:        node,
			NodeKind:    nodeKind,
			DocTemplate: docTemplate,
			Goal:        nodeGoal,
			ConceptKeys: nodeConceptKeys,
			PrereqKeys:  prereqKeys,
			ConceptCSV:  conceptCSV,
			Meta:        nodeMeta,
		}
	}

	patternHierarchyJSON := ""
	if v, ok := pathMeta["pattern_hierarchy"]; ok && v != nil {
		if pb, err := json.Marshal(v); err == nil {
			patternHierarchyJSON = string(pb)
		}
	}
	if patternHierarchyJSON == "" {
		if pb, err := json.Marshal(patternHierarchy); err == nil {
			patternHierarchyJSON = string(pb)
		}
	}

	pathStructureJSON := ""
	if len(nodes) > 0 {
		if pb, err := json.Marshal(pathStructureSummary(nodes)); err == nil {
			pathStructureJSON = string(pb)
		}
	}

	styleManifestJSON := ""
	if js, updates := ensureStyleManifest(ctx, deps, pathID, pathMeta, pathIntentMD, pathStyleJSON, patternHierarchyJSON, pathStructureJSON); js != "" {
		styleManifestJSON = js
		if len(updates) > 0 {
			updatePathMeta(ctx, deps, pathID, pathMeta, updates)
			for k, v := range updates {
				pathMeta[k] = v
			}
		}
	}

	pathNarrativeJSON := ""
	if js, updates := ensurePathNarrativePlan(ctx, deps, pathID, pathMeta, pathIntentMD, patternHierarchyJSON, pathStructureJSON, styleManifestJSON); js != "" {
		pathNarrativeJSON = js
		if len(updates) > 0 {
			updatePathMeta(ctx, deps, pathID, pathMeta, updates)
			for k, v := range updates {
				pathMeta[k] = v
			}
		}
	}

	styleManifestPromptJSON := strings.TrimSpace(styleManifestJSON)
	if styleManifestPromptJSON == "" {
		styleManifestPromptJSON = "(none)"
	}
	pathNarrativePromptJSON := strings.TrimSpace(pathNarrativeJSON)
	if pathNarrativePromptJSON == "" {
		pathNarrativePromptJSON = "(none)"
	}

	type nodeWork struct {
		Node        *types.PathNode
		NodeKind    string
		DocTemplate string
		Goal        string
		ConceptKeys []string
		PrereqKeys  []string
		ConceptCSV  string
		// Prompt-friendly knowledge graph context for ConceptKeys (cross-path mastery transfer).
		UserKnowledgeJSON    string
		TeachingPatternsJSON string
		QueryText            string
		QueryEmb             []float32
	}
	work := make([]nodeWork, 0, len(nodes))
	for _, info := range infoByID {
		if info.Node == nil || info.Node.ID == uuid.Nil {
			continue
		}
		if hasDoc[info.Node.ID] {
			out.DocsExisting++
			continue
		}
		queryText := strings.TrimSpace(info.Node.Title + " " + info.Goal + " " + info.ConceptCSV)
		work = append(work, nodeWork{
			Node:        info.Node,
			NodeKind:    info.NodeKind,
			DocTemplate: info.DocTemplate,
			Goal:        info.Goal,
			ConceptKeys: info.ConceptKeys,
			PrereqKeys:  info.PrereqKeys,
			ConceptCSV:  info.ConceptCSV,
			QueryText:   queryText,
		})
	}
	if len(work) == 0 {
		return out, nil
	}

	patternContextByNodeID := map[uuid.UUID]string{}
	for id, info := range infoByID {
		if info.Node == nil || info.Node.ID == uuid.Nil {
			continue
		}
		ctxJSON := patternContextJSONForNode(info.Node, nodesByID, patternHierarchy)
		if strings.TrimSpace(ctxJSON) == "" {
			ctxJSON = "(none)"
		}
		patternContextByNodeID[id] = ctxJSON
	}

	// Load concepts once for both user-knowledge context and narrative graph hints.
	var (
		concepts         []*types.Concept
		conceptByKey     = map[string]*types.Concept{}
		canonicalIDByKey = map[string]uuid.UUID{}
		conceptIDs       []uuid.UUID
	)
	if deps.Concepts != nil {
		if rows, err := deps.Concepts.GetByScope(dbctx.Context{Ctx: ctx}, "path", &pathID); err == nil {
			concepts = rows
			for _, c := range concepts {
				if c == nil || c.ID == uuid.Nil {
					continue
				}
				k := strings.TrimSpace(strings.ToLower(c.Key))
				if k == "" {
					continue
				}
				conceptByKey[k] = c
				id := c.ID
				if c.CanonicalConceptID != nil && *c.CanonicalConceptID != uuid.Nil {
					id = *c.CanonicalConceptID
				}
				if id != uuid.Nil {
					canonicalIDByKey[k] = id
				}
				conceptIDs = append(conceptIDs, c.ID)
			}
		} else if deps.Log != nil {
			deps.Log.Warn("node_doc_build: failed to load concepts (continuing)", "error", err, "path_id", pathID.String())
		}
	}

	// ---- User knowledge context (cross-path mastery transfer) ----
	//
	// Best-effort: map node concept keys -> canonical concept IDs -> user concept state rows.
	// We compute this once per run and attach a compact JSON context per node to keep prompts consistent and scalable.
	if deps.ConceptState != nil && len(canonicalIDByKey) > 0 {
		needed := map[uuid.UUID]bool{}
		for _, w := range work {
			for _, k := range w.ConceptKeys {
				k = strings.TrimSpace(strings.ToLower(k))
				if k == "" {
					continue
				}
				if id := canonicalIDByKey[k]; id != uuid.Nil {
					needed[id] = true
				}
			}
		}

		ids := make([]uuid.UUID, 0, len(needed))
		for id := range needed {
			if id != uuid.Nil {
				ids = append(ids, id)
			}
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })

		stateByConceptID := map[uuid.UUID]*types.UserConceptState{}
		if len(ids) > 0 {
			if rows, err := deps.ConceptState.ListByUserAndConceptIDs(dbctx.Context{Ctx: ctx}, in.OwnerUserID, ids); err == nil {
				for _, st := range rows {
					if st == nil || st.ConceptID == uuid.Nil {
						continue
					}
					stateByConceptID[st.ConceptID] = st
				}
			} else if deps.Log != nil {
				deps.Log.Warn("node_doc_build: failed to load user concept states (continuing)", "error", err, "user_id", in.OwnerUserID.String())
			}
		}

		now := time.Now().UTC()
		for i := range work {
			uj := BuildUserKnowledgeContextV1(work[i].ConceptKeys, canonicalIDByKey, stateByConceptID, now).JSON()
			if strings.TrimSpace(uj) == "" {
				uj = "(none)"
			}
			work[i].UserKnowledgeJSON = uj
		}
	} else {
		for i := range work {
			work[i].UserKnowledgeJSON = "(none)"
		}
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

	// Pre-fetch teaching patterns once per node (avoid repeating per retry).
	if deps.TeachingPatterns != nil {
		prefetchConc := 8
		if prefetchConc > len(work) {
			prefetchConc = len(work)
		}
		if prefetchConc < 1 {
			prefetchConc = 1
		}
		tg, tctx := errgroup.WithContext(ctx)
		tg.SetLimit(prefetchConc)
		for i := range work {
			i := i
			tg.Go(func() error {
				if err := tctx.Err(); err != nil {
					return err
				}
				teachingJSON, _ := teachingPatternsJSON(tctx, deps.Vec, deps.TeachingPatterns, work[i].QueryEmb, 4)
				if strings.TrimSpace(teachingJSON) == "" {
					teachingJSON = "(none)"
				}
				work[i].TeachingPatternsJSON = teachingJSON
				return nil
			})
		}
		if err := tg.Wait(); err != nil && tctx.Err() != nil {
			return out, err
		}
	} else {
		for i := range work {
			work[i].TeachingPatternsJSON = "(none)"
		}
	}

	// ---- Narrative scaffolding (prev/next/module + concept graph hints) ----
	orderedIDs := make([]uuid.UUID, 0, len(nodes))
	for _, n := range nodes {
		if n != nil && n.ID != uuid.Nil {
			orderedIDs = append(orderedIDs, n.ID)
		}
	}
	isModule := map[uuid.UUID]bool{}
	for id, info := range infoByID {
		if info.NodeKind == "module" {
			isModule[id] = true
		}
	}
	buildPrevNext := func(ids []uuid.UUID) (map[uuid.UUID]uuid.UUID, map[uuid.UUID]uuid.UUID) {
		prev := map[uuid.UUID]uuid.UUID{}
		next := map[uuid.UUID]uuid.UUID{}
		var last uuid.UUID
		for _, id := range ids {
			if id == uuid.Nil {
				continue
			}
			if last != uuid.Nil {
				prev[id] = last
				next[last] = id
			}
			last = id
		}
		return prev, next
	}
	lessonIDs := make([]uuid.UUID, 0, len(orderedIDs))
	moduleIDs := make([]uuid.UUID, 0, len(orderedIDs))
	for _, id := range orderedIDs {
		if isModule[id] {
			moduleIDs = append(moduleIDs, id)
		} else {
			lessonIDs = append(lessonIDs, id)
		}
	}
	prevLesson, nextLesson := buildPrevNext(lessonIDs)
	prevModule, nextModule := buildPrevNext(moduleIDs)

	parentByID := map[uuid.UUID]*uuid.UUID{}
	childrenByParent := map[uuid.UUID][]uuid.UUID{}
	for id, info := range infoByID {
		if info.Node == nil || info.Node.ID == uuid.Nil {
			continue
		}
		if info.Node.ParentNodeID != nil && *info.Node.ParentNodeID != uuid.Nil {
			pid := *info.Node.ParentNodeID
			parentByID[id] = &pid
			childrenByParent[pid] = append(childrenByParent[pid], id)
		}
	}
	for pid, kids := range childrenByParent {
		sort.Slice(kids, func(i, j int) bool {
			ai := infoByID[kids[i]].Node.Index
			aj := infoByID[kids[j]].Node.Index
			return ai < aj
		})
		childrenByParent[pid] = kids
	}

	moduleIDByNodeID := map[uuid.UUID]uuid.UUID{}
	moduleTitleByNodeID := map[uuid.UUID]string{}
	moduleGoalByNodeID := map[uuid.UUID]string{}
	moduleSiblingsByNodeID := map[uuid.UUID][]string{}
	for id, info := range infoByID {
		if info.Node == nil || info.Node.ID == uuid.Nil {
			continue
		}
		curr := id
		var moduleID uuid.UUID
		for {
			curInfo, ok := infoByID[curr]
			if ok && curInfo.NodeKind == "module" {
				moduleID = curr
				break
			}
			parent := parentByID[curr]
			if parent == nil || *parent == uuid.Nil {
				break
			}
			curr = *parent
		}
		if moduleID == uuid.Nil {
			continue
		}
		moduleIDByNodeID[id] = moduleID
		moduleInfo := infoByID[moduleID]
		moduleTitleByNodeID[id] = strings.TrimSpace(moduleInfo.Node.Title)
		moduleGoalByNodeID[id] = strings.TrimSpace(moduleInfo.Goal)
		sibs := make([]string, 0)
		for _, sid := range childrenByParent[moduleID] {
			if sib := infoByID[sid].Node; sib != nil {
				title := strings.TrimSpace(sib.Title)
				if title != "" {
					sibs = append(sibs, title)
				}
			}
		}
		moduleSiblingsByNodeID[id] = dedupeStrings(sibs)
	}

	// Concept graph hints (prereq / related / analogy).
	edgeByTo := map[uuid.UUID][]*types.ConceptEdge{}
	edgeByFrom := map[uuid.UUID][]*types.ConceptEdge{}
	if deps.Edges != nil && len(conceptIDs) > 0 {
		if rows, err := deps.Edges.GetByConceptIDs(dbctx.Context{Ctx: ctx}, conceptIDs); err == nil {
			for _, e := range rows {
				if e == nil || e.FromConceptID == uuid.Nil || e.ToConceptID == uuid.Nil {
					continue
				}
				edgeByTo[e.ToConceptID] = append(edgeByTo[e.ToConceptID], e)
				edgeByFrom[e.FromConceptID] = append(edgeByFrom[e.FromConceptID], e)
			}
		} else if deps.Log != nil {
			deps.Log.Warn("node_doc_build: failed to load concept edges (continuing)", "error", err, "path_id", pathID.String())
		}
	}

	conceptBriefForID := func(id uuid.UUID) (conceptBrief, bool) {
		if id == uuid.Nil {
			return conceptBrief{}, false
		}
		for _, c := range concepts {
			if c == nil || c.ID != id {
				continue
			}
			k := strings.TrimSpace(c.Key)
			name := strings.TrimSpace(c.Name)
			if k == "" {
				return conceptBrief{}, false
			}
			return conceptBrief{Key: k, Name: name}, true
		}
		return conceptBrief{}, false
	}

	graphHintsByNodeID := map[uuid.UUID]nodeNarrativeContext{}
	for id, info := range infoByID {
		if info.Node == nil || info.Node.ID == uuid.Nil {
			continue
		}
		prereq := map[string]conceptBrief{}
		related := map[string]conceptBrief{}
		analogy := map[string]conceptBrief{}

		for _, k := range info.PrereqKeys {
			ck := strings.TrimSpace(strings.ToLower(k))
			if ck == "" {
				continue
			}
			if c := conceptByKey[ck]; c != nil {
				prereq[ck] = conceptBrief{Key: strings.TrimSpace(c.Key), Name: strings.TrimSpace(c.Name)}
			}
		}

		conceptIDsForNode := make([]uuid.UUID, 0, len(info.ConceptKeys))
		for _, k := range info.ConceptKeys {
			ck := strings.TrimSpace(strings.ToLower(k))
			if ck == "" {
				continue
			}
			if c := conceptByKey[ck]; c != nil && c.ID != uuid.Nil {
				conceptIDsForNode = append(conceptIDsForNode, c.ID)
			}
		}

		for _, cid := range conceptIDsForNode {
			for _, e := range edgeByTo[cid] {
				if e == nil {
					continue
				}
				et := strings.TrimSpace(strings.ToLower(e.EdgeType))
				if et == "prereq" {
					if cb, ok := conceptBriefForID(e.FromConceptID); ok {
						prereq[strings.ToLower(cb.Key)] = cb
					}
				} else if et == "related" {
					if cb, ok := conceptBriefForID(e.FromConceptID); ok {
						related[strings.ToLower(cb.Key)] = cb
					}
				} else if et == "analogy" {
					if cb, ok := conceptBriefForID(e.FromConceptID); ok {
						analogy[strings.ToLower(cb.Key)] = cb
					}
				}
			}
			for _, e := range edgeByFrom[cid] {
				if e == nil {
					continue
				}
				et := strings.TrimSpace(strings.ToLower(e.EdgeType))
				if et == "related" {
					if cb, ok := conceptBriefForID(e.ToConceptID); ok {
						related[strings.ToLower(cb.Key)] = cb
					}
				} else if et == "analogy" {
					if cb, ok := conceptBriefForID(e.ToConceptID); ok {
						analogy[strings.ToLower(cb.Key)] = cb
					}
				}
			}
		}

		toSlice := func(m map[string]conceptBrief, max int) []conceptBrief {
			out := make([]conceptBrief, 0, len(m))
			for _, v := range m {
				out = append(out, v)
			}
			sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
			if max > 0 && len(out) > max {
				out = out[:max]
			}
			return out
		}

		graphHintsByNodeID[id] = nodeNarrativeContext{
			PrereqConcepts:  toSlice(prereq, 6),
			RelatedConcepts: toSlice(related, 6),
			AnalogyConcepts: toSlice(analogy, 4),
		}
	}

	// Thread summaries (existing docs + metadata fallback).
	threadByNodeID := map[uuid.UUID]nodeThreadSummary{}
	for _, d := range existingDocs {
		if d == nil || d.PathNodeID == uuid.Nil || len(d.DocJSON) == 0 || string(d.DocJSON) == "null" {
			continue
		}
		var doc content.NodeDocV1
		if err := json.Unmarshal(d.DocJSON, &doc); err != nil {
			continue
		}
		info := infoByID[d.PathNodeID]
		threadByNodeID[d.PathNodeID] = nodeThreadSummary{
			Title:       strings.TrimSpace(info.Node.Title),
			Summary:     strings.TrimSpace(doc.Summary),
			KeyTerms:    dedupeStrings(doc.ConceptKeys),
			ConceptKeys: info.ConceptKeys,
		}
	}
	for id, info := range infoByID {
		if _, ok := threadByNodeID[id]; ok {
			continue
		}
		summary := strings.TrimSpace(info.Goal)
		if summary == "" {
			summary = strings.TrimSpace(info.Node.Title)
		}
		threadByNodeID[id] = nodeThreadSummary{
			Title:       strings.TrimSpace(info.Node.Title),
			Summary:     summary,
			KeyTerms:    dedupeStrings(info.ConceptKeys),
			ConceptKeys: info.ConceptKeys,
		}
	}

	maxConc := envInt("NODE_DOC_BUILD_CONCURRENCY", 4)
	if maxConc < 1 {
		maxConc = 1
	}

	// Outline generation (parallel, best-effort).
	outlineSchema, oerr := schema.NodeDocOutlineV1()
	if oerr != nil {
		return out, oerr
	}
	outlineByNodeID := map[uuid.UUID]content.NodeDocOutlineV1{}
	outlineConc := envInt("NODE_DOC_OUTLINE_CONCURRENCY", maxConc)
	if outlineConc < 1 {
		outlineConc = 1
	}
	og, octx := errgroup.WithContext(ctx)
	og.SetLimit(outlineConc)
	for i := range work {
		w := work[i]
		og.Go(func() error {
			if w.Node == nil || w.Node.ID == uuid.Nil {
				return nil
			}

			prevID := prevLesson[w.Node.ID]
			nextID := nextLesson[w.Node.ID]
			if isModule[w.Node.ID] {
				prevID = prevModule[w.Node.ID]
				nextID = nextModule[w.Node.ID]
			}
			prevSum := threadByNodeID[prevID]
			nextSum := threadByNodeID[nextID]

			nctx := nodeNarrativeContext{
				PrevTitle:       prevSum.Title,
				PrevSummary:     prevSum.Summary,
				PrevKeyTerms:    prevSum.KeyTerms,
				NextTitle:       nextSum.Title,
				NextSummary:     nextSum.Summary,
				NextKeyTerms:    nextSum.KeyTerms,
				ModuleTitle:     moduleTitleByNodeID[w.Node.ID],
				ModuleGoal:      moduleGoalByNodeID[w.Node.ID],
				ModuleSiblings:  moduleSiblingsByNodeID[w.Node.ID],
				PrereqConcepts:  graphHintsByNodeID[w.Node.ID].PrereqConcepts,
				RelatedConcepts: graphHintsByNodeID[w.Node.ID].RelatedConcepts,
				AnalogyConcepts: graphHintsByNodeID[w.Node.ID].AnalogyConcepts,
			}
			nctxJSON, _ := json.Marshal(nctx)

			system := strings.TrimSpace(`
MODE: NODE_DOC_OUTLINE

You create a research-grade lesson outline with a clear narrative arc.
Rules:
- Output ONLY valid JSON that matches the schema.
- First section MUST be "Roadmap".
- 4–8 sections total, ordered from intuition -> core idea -> worked example -> practice/pitfalls -> wrap-up.
- Provide bridge_in and bridge_out sentences that connect sections naturally.
- bridge_in/bridge_out must be learner-facing and content-focused; do NOT mention outlines, plans, modules, paths, or lesson structure.
- Avoid meta phrasing like "up next", "next lesson", "wrap-up", "bridge-in/out", "your next hop", or "you've seen the plan".
- concept_keys per section should be a subset of the node's concepts (include prereqs when needed).
- If PATTERN_CONTEXT_JSON is provided, align section flow to the selected opening/core/practice/closing patterns.
- If STYLE_MANIFEST_JSON is provided, align tone and phrasing to its guidance.
- If PATH_NARRATIVE_PLAN_JSON is provided, follow its continuity rules for bridge_in/out and thread_summary.
- thread_summary is a 1–2 sentence throughline that connects this lesson to previous and next nodes.
- key_terms are 3–7 short nouns/phrases.
- prereq_recap is 1–2 sentences that explicitly references any prereq concepts.
- next_preview is 1 sentence that previews the next lesson; if a title is provided, reference the title directly without phrases like "next lesson".
`)

			user := fmt.Sprintf(`
NODE_TITLE: %s
NODE_GOAL: %s
NODE_KIND: %s
DOC_TEMPLATE: %s
CONCEPT_KEYS: %s
PREREQ_CONCEPT_KEYS: %s

NARRATIVE_CONTEXT_JSON:
%s

PATH_INTENT_MD:
%s

PATH_STYLE_JSON (optional):
%s

STYLE_MANIFEST_JSON (optional):
%s

PATH_NARRATIVE_PLAN_JSON (optional):
%s

PATTERN_CONTEXT_JSON (optional; path/module/lesson teaching patterns):
%s
`,
				w.Node.Title,
				w.Goal,
				w.NodeKind,
				w.DocTemplate,
				w.ConceptCSV,
				strings.Join(w.PrereqKeys, ", "),
				string(nctxJSON),
				strings.TrimSpace(pathIntentMD),
				strings.TrimSpace(pathStyleJSON),
				styleManifestPromptJSON,
				pathNarrativePromptJSON,
				patternContextByNodeID[w.Node.ID],
			)

			obj, err := deps.AI.GenerateJSON(octx, system, user, "node_doc_outline_v1", outlineSchema)
			if err != nil {
				// Fallback to a minimal outline.
				outlineByNodeID[w.Node.ID] = normalizeOutline(content.NodeDocOutlineV1{}, w.Node.Title, w.ConceptKeys)
				return nil
			}
			raw, _ := json.Marshal(obj)
			var outline content.NodeDocOutlineV1
			if err := json.Unmarshal(raw, &outline); err != nil {
				outlineByNodeID[w.Node.ID] = normalizeOutline(content.NodeDocOutlineV1{}, w.Node.Title, w.ConceptKeys)
				return nil
			}
			outlineByNodeID[w.Node.ID] = normalizeOutline(outline, w.Node.Title, w.ConceptKeys)
			return nil
		})
	}
	if err := og.Wait(); err != nil && octx.Err() != nil {
		return out, err
	}

	// Update thread summaries using outlines (for nodes we are building now).
	for _, w := range work {
		if w.Node == nil || w.Node.ID == uuid.Nil {
			continue
		}
		ol := outlineByNodeID[w.Node.ID]
		if strings.TrimSpace(ol.ThreadSummary) != "" {
			threadByNodeID[w.Node.ID] = nodeThreadSummary{
				Title:       strings.TrimSpace(w.Node.Title),
				Summary:     strings.TrimSpace(ol.ThreadSummary),
				KeyTerms:    dedupeStrings(ol.KeyTerms),
				ConceptKeys: w.ConceptKeys,
			}
		}
	}

	nodeNarrativeByNodeID := map[uuid.UUID]string{}
	if envBool("NODE_NARRATIVE_ENABLED", true) && deps.AI != nil {
		narrConc := envInt("NODE_NARRATIVE_CONCURRENCY", maxConc)
		if narrConc < 1 {
			narrConc = 1
		}
		ng, nctx := errgroup.WithContext(ctx)
		ng.SetLimit(narrConc)
		var nmu sync.Mutex
		for i := range work {
			w := work[i]
			ng.Go(func() error {
				if w.Node == nil || w.Node.ID == uuid.Nil {
					return nil
				}

				outline := outlineByNodeID[w.Node.ID]
				outline = normalizeOutline(outline, w.Node.Title, w.ConceptKeys)
				outlineJSON, _ := json.Marshal(outline)

				prevID := prevLesson[w.Node.ID]
				nextID := nextLesson[w.Node.ID]
				if isModule[w.Node.ID] {
					prevID = prevModule[w.Node.ID]
					nextID = nextModule[w.Node.ID]
				}
				prevTitle := strings.TrimSpace(threadByNodeID[prevID].Title)
				nextTitle := strings.TrimSpace(threadByNodeID[nextID].Title)
				moduleTitle := strings.TrimSpace(moduleTitleByNodeID[w.Node.ID])

				nodeMeta := infoByID[w.Node.ID].Meta
				js, updates := ensureNodeNarrativePlan(
					nctx,
					deps,
					w.Node,
					nodeMeta,
					pathNarrativeJSON,
					styleManifestJSON,
					string(outlineJSON),
					w.ConceptCSV,
					prevTitle,
					nextTitle,
					moduleTitle,
				)
				if strings.TrimSpace(js) != "" {
					nmu.Lock()
					nodeNarrativeByNodeID[w.Node.ID] = js
					nmu.Unlock()
				}
				if len(updates) > 0 {
					updateNodeMeta(ctx, deps, w.Node.ID, nodeMeta, updates)
				}
				return nil
			})
		}
		if err := ng.Wait(); err != nil && nctx.Err() != nil {
			return out, err
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
	strictNarrative := false
	switch qualityMode() {
	case "premium", "openai", "high":
		strictNarrative = true
	}

	// Derived material sets share the chunk namespace (and KG products) with their source upload batch.
	sourceSetID := in.MaterialSetID
	if deps.DB != nil {
		if sc, err := materialsetctx.Resolve(ctx, deps.DB, in.MaterialSetID); err == nil && sc.SourceMaterialSetID != uuid.Nil {
			sourceSetID = sc.SourceMaterialSetID
		}
	}
	chunksNS := index.ChunksNamespace(sourceSetID)

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

			outline := outlineByNodeID[w.Node.ID]
			outline = normalizeOutline(outline, w.Node.Title, w.ConceptKeys)

			prevID := prevLesson[w.Node.ID]
			nextID := nextLesson[w.Node.ID]
			if isModule[w.Node.ID] {
				prevID = prevModule[w.Node.ID]
				nextID = nextModule[w.Node.ID]
			}
			prevSum := threadByNodeID[prevID]
			nextSum := threadByNodeID[nextID]

			prevTitle := strings.TrimSpace(prevSum.Title)
			nextTitle := strings.TrimSpace(nextSum.Title)
			moduleTitle := strings.TrimSpace(moduleTitleByNodeID[w.Node.ID])

			nctx := nodeNarrativeContext{
				PrevTitle:       prevSum.Title,
				PrevSummary:     prevSum.Summary,
				PrevKeyTerms:    prevSum.KeyTerms,
				NextTitle:       nextSum.Title,
				NextSummary:     nextSum.Summary,
				NextKeyTerms:    nextSum.KeyTerms,
				ModuleTitle:     moduleTitle,
				ModuleGoal:      moduleGoalByNodeID[w.Node.ID],
				ModuleSiblings:  moduleSiblingsByNodeID[w.Node.ID],
				PrereqConcepts:  graphHintsByNodeID[w.Node.ID].PrereqConcepts,
				RelatedConcepts: graphHintsByNodeID[w.Node.ID].RelatedConcepts,
				AnalogyConcepts: graphHintsByNodeID[w.Node.ID].AnalogyConcepts,
			}
			nctxJSON, _ := json.Marshal(nctx)

			sections := outline.Sections
			if len(sections) == 0 {
				sections = normalizeOutline(content.NodeDocOutlineV1{}, w.Node.Title, w.ConceptKeys).Sections
			}
			sectionQueries := make([]string, 0, len(sections))
			for _, sec := range sections {
				parts := []string{
					w.Node.Title,
					w.Goal,
					sec.Heading,
					sec.Goal,
					strings.Join(sec.ConceptKeys, ", "),
				}
				sectionQueries = append(sectionQueries, strings.TrimSpace(strings.Join(parts, " ")))
			}

			sectionEmbs, err := deps.AI.Embed(gctx, sectionQueries)
			if err != nil || len(sectionEmbs) != len(sections) {
				sectionEmbs = nil
			}

			sectionEvidenceList := make([]sectionEvidence, len(sections))
			sectionChunkIDs := make([][]uuid.UUID, len(sections))

			sectionConc := envInt("NODE_DOC_SECTION_RETRIEVAL_CONCURRENCY", 6)
			if sectionConc < 1 {
				sectionConc = 1
			}
			sg, sctx := errgroup.WithContext(gctx)
			sg.SetLimit(sectionConc)

			for i := range sections {
				i := i
				sg.Go(func() error {
					sec := sections[i]
					qEmb := w.QueryEmb
					if sectionEmbs != nil && i < len(sectionEmbs) && len(sectionEmbs[i]) > 0 {
						qEmb = sectionEmbs[i]
					}

					const semanticK = 14
					const lexicalK = 8
					const finalK = 14

					ids, _, _ := graphAssistedChunkIDs(sctx, deps.DB, deps.Vec, chunkRetrievePlan{
						MaterialSetID: sourceSetID,
						ChunksNS:      chunksNS,
						QueryText:     sectionQueries[i],
						QueryEmb:      qEmb,
						FileIDs:       fileIDs,
						AllowFiles:    allowFiles,
						SeedK:         semanticK,
						LexicalK:      lexicalK,
						FinalK:        finalK,
					})
					ids = dedupeUUIDsPreserveOrder(ids)

					if len(ids) < finalK {
						ce, err := buildChunkEmbs()
						if err == nil {
							fallback := topKChunkIDsByCosine(qEmb, ce, finalK)
							ids = dedupeUUIDsPreserveOrder(append(ids, fallback...))
						}
					}
					if len(ids) > finalK {
						ids = ids[:finalK]
					}

					ex := buildExcerpts(ids, 10, 750)
					if strings.TrimSpace(ex) == "" && len(ids) == 0 {
						ex = buildExcerpts(ids, 10, 750)
					}
					sectionChunkIDs[i] = ids
					sectionEvidenceList[i] = sectionEvidence{
						Heading:     sec.Heading,
						Goal:        sec.Goal,
						ConceptKeys: sec.ConceptKeys,
						BridgeIn:    sec.BridgeIn,
						BridgeOut:   sec.BridgeOut,
						ChunkIDs:    uuidStrings(ids),
						Excerpts:    ex,
					}
					return nil
				})
			}
			if err := sg.Wait(); err != nil && sctx.Err() != nil {
				return err
			}

			union := make([]uuid.UUID, 0)
			for _, ids := range sectionChunkIDs {
				union = append(union, ids...)
			}
			union = dedupeUUIDsPreserveOrder(union)

			// Ensure full coverage: include any must-cite chunks assigned to this node.
			mustCiteIDs := mustCiteByNodeID[w.Node.ID]
			figCiteIDs := figChunkIDsByNode[w.Node.ID]
			vidCiteIDs := vidChunkIDsByNode[w.Node.ID]
			chunkIDs := mergeUUIDListsPreserveOrder(mustCiteIDs, figCiteIDs, vidCiteIDs, union)
			if len(chunkIDs) > 40 {
				chunkIDs = chunkIDs[:40]
			}

			excerpts := buildExcerpts(chunkIDs, 24, 900)
			if strings.TrimSpace(excerpts) == "" {
				return fmt.Errorf("node_doc_build: empty grounding excerpts")
			}
			equationsJSON := buildEquationsJSON(chunkByID, chunkIDs)

			sectionEvidenceJSON, _ := json.Marshal(sectionEvidenceList)
			outlineJSON, _ := json.Marshal(outline)

			extras := make([]*mediaAssetCandidate, 0, len(figAssetsByNode[w.Node.ID])+len(vidAssetsByNode[w.Node.ID]))
			extras = append(extras, figAssetsByNode[w.Node.ID]...)
			extras = append(extras, vidAssetsByNode[w.Node.ID]...)
			assetsJSON, availableAssets := buildAvailableAssetsJSON(deps.Bucket, files, chunkByID, chunkIDs, extras)

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

			nodeNarrativeRaw := strings.TrimSpace(nodeNarrativeByNodeID[w.Node.ID])
			nodeNarrativeJSON := nodeNarrativeRaw
			if nodeNarrativeJSON == "" {
				nodeNarrativeJSON = "(none)"
			}
			mediaRankJSON := "(none)"
			nodeMeta := infoByID[w.Node.ID].Meta
			if js, updates := ensureMediaRank(gctx, deps, w.Node, nodeMeta, string(outlineJSON), assetsJSON, nodeNarrativeRaw, styleManifestJSON); strings.TrimSpace(js) != "" {
				mediaRankJSON = js
				if len(updates) > 0 {
					updateNodeMeta(ctx, deps, w.Node.ID, nodeMeta, updates)
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
		- If STYLE_MANIFEST_JSON is provided, follow its tone/register and phrase guidance.
		- Do not output raw concept keys (snake_case). Write concept names in natural language.
	- Summary is already provided via the summary field; do NOT include a "Summary" heading/section that repeats it.
	  If you include an ending recap, title it "Key takeaways" and make it additive (not a rephrase).

	Pedagogy:
	- If TEACHING_PATTERNS_JSON is provided, pick 1-2 patterns and apply them (without naming keys) so the lesson teaches, not a page of facts.
	- Spread quick checks throughout the doc (not all at the end).
	- Teach before test: place each quick_check only after you've already taught the idea it checks (never before introducing the referenced definition/step).

	Narrative flow (non-negotiable):
	- Follow OUTLINE_JSON exactly; use each section heading verbatim.
	- Use bridge_in and bridge_out to connect sections naturally (don’t skip).
	- If PREV_NODE_TITLE is provided, mention it verbatim in the first 1–2 blocks to connect continuity.
	- If MODULE_TITLE is provided, mention it verbatim early to anchor the module thread.
	- If NEXT_NODE_TITLE is provided, mention it verbatim near the end to preview what’s next.
	- Objectives and prerequisites must appear before the Roadmap section. Key takeaways must appear near the end.
	- If PATTERN_CONTEXT_JSON is provided, follow its opening/core/example/visual/practice/closing/depth/engagement patterns.
	- If PATH_NARRATIVE_PLAN_JSON is provided, follow its continuity rules and preferred transitions.
	- If NODE_NARRATIVE_PLAN_JSON is provided, honor its opening/closing intent and anchor terms.
	- Do not mention pattern names or keys explicitly; use them as internal guidance.
	- Use CONCEPT_KEYS as the doc's concept_keys; do not leave concept_keys empty.

	Evidence usage:
	- Use SECTION_EXCERPTS_JSON to ground each section; cite only chunk_ids relevant to that section.
	- Use short quotes sparingly when they clarify; a quote callout (variant="quote") is allowed.

		Media rules (diagrams vs figures):
		- "diagram" blocks are SVG/Mermaid and are best for precise, labeled, math-y visuals (flows, free-body diagrams, graphs).
		- SVG diagrams may include simple <animate>/<animateTransform> to illustrate motion or step transitions (no scripts; keep it subtle).
		- If diagram.kind="mermaid": diagram.source MUST be ONLY the Mermaid spec (no prose, no "Diagram" label, no backticks/code fences). Put explanation in diagram.caption.
		- If diagram.kind="svg": diagram.source MUST be ONLY a standalone <svg>…</svg> string (no prose). Put explanation in diagram.caption.
		- "figure" blocks are raster images and are best for higher-fidelity intuition, setups, and real-world context (“vibes”) where diagrams fall short.
		- If MEDIA_RANK_JSON is provided, prefer its recommended asset URLs for the matching sections.
	- Do NOT put labels/text inside figures; keep labels in captions and use diagrams for labeled visuals.

	Schema contract:
	- Use "order" as the only render order. Each order item is {kind,id}.
	- Each order item must reference exactly one object with the same id in the corresponding array:
	  headings | paragraphs | callouts | codes | figures | videos | diagrams | tables | equations | objectives | prerequisites | key_takeaways | glossary | common_mistakes | misconceptions | edge_cases | heuristics | steps | checklist | faq | intuition | mental_model | why_it_matters | connections | quick_checks | dividers.
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
	- equations:
	  - Use latex for the formula, display=true for block equations and display=false for inline equations.
	  - If a placeholder like [[EQ1]] appears in excerpts, replace it with an equation block (do not leave the placeholder in text).
		- faq:
		  - Use qas: {question_md, answer_md}. Keep answers tight; no rambling.
		- intuition/mental_model/why_it_matters:
		  - Use md: a short, vivid section that adds insight (not generic filler).
		  - For intuition and mental_model blocks: if a visualization would materially improve comprehension or memory, include a supporting visual block immediately before/after it (figure/video/diagram) and make the visual caption explicitly reinforce that specific intuition/mental model.

			Hard rules:
			- Output ONLY valid JSON that matches the schema. No surrounding text.
			- Do not include planning/meta/check-in language or backend scaffolding.
			- Do not mention STYLE_MANIFEST_JSON, PATH_NARRATIVE_PLAN_JSON, NODE_NARRATIVE_PLAN_JSON, or MEDIA_RANK_JSON.
			- Never reference the outline, sections, templates, nodes, modules, paths, bridges, or "next hop".
			- Do not use headings like "Recommended drills", "Summary", "Wrap-up", or "Reveal answer".
			- You MAY reference previous/next lesson titles (if provided) only as content continuity, not navigation.
			- Keep tone precise and professional; avoid hype, cutesy metaphors, or condescending phrasing.
		- Quick checks may be short-answer, true/false, or multiple choice.
		  - For quick_checks items, ALWAYS include: kind, options, answer_id (schema requires them).
		  - short_answer: kind="short_answer", options=[], answer_id=""; answer_md is the reference answer/explanation.
		  - true_false: kind="true_false", options=[{id:"A",text:"True"},{id:"B",text:"False"}], answer_id="A"|"B".
		  - mcq: kind="mcq", options has 3-5 options, answer_id matches one option id, answer_md explains why.
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

				teachingJSON := strings.TrimSpace(w.TeachingPatternsJSON)
				if teachingJSON == "" {
					teachingJSON = "(none)"
				}

				user := fmt.Sprintf(`
NODE_TITLE: %s
NODE_GOAL: %s
CONCEPT_KEYS: %s
NODE_KIND: %s
DOC_TEMPLATE: %s
PREV_NODE_TITLE (optional; use verbatim if provided):
%s
NEXT_NODE_TITLE (optional; use verbatim if provided):
%s
MODULE_TITLE (optional; use verbatim if provided):
%s

PATTERN_CONTEXT_JSON (optional; path/module/lesson teaching patterns):
%s

STYLE_MANIFEST_JSON (optional):
%s

PATH_NARRATIVE_PLAN_JSON (optional):
%s

NODE_NARRATIVE_PLAN_JSON (optional):
%s

MEDIA_RANK_JSON (optional; recommended assets per section):
%s

OUTLINE_JSON (section headings + goals + bridges; follow exactly):
%s

SECTION_EXCERPTS_JSON (per section chunk_ids + excerpts; use for grounding):
%s

NARRATIVE_CONTEXT_JSON (prev/next summaries + module + graph hints):
%s

EQUATIONS_JSON (optional; per chunk placeholders + LaTeX):
%s

USER_KNOWLEDGE_JSON (optional; mastery/exposure for CONCEPT_KEYS; do not mention explicitly):
%s

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
Return ONLY JSON matching schema.`,
					w.Node.Title,
					w.Goal,
					w.ConceptCSV,
					w.NodeKind,
					w.DocTemplate,
					prevTitle,
					nextTitle,
					moduleTitle,
					patternContextByNodeID[w.Node.ID],
					styleManifestPromptJSON,
					pathNarrativePromptJSON,
					strings.TrimSpace(nodeNarrativeJSON),
					strings.TrimSpace(mediaRankJSON),
					string(outlineJSON),
					string(sectionEvidenceJSON),
					string(nctxJSON),
					equationsJSON,
					w.UserKnowledgeJSON,
					userProfileDoc,
					teachingJSON,
					intentForPrompt,
					formatChunkIDBullets(mustCiteIDs),
					reqs.MinWordCount,
					reqs.MinParagraphs,
					reqs.MinCallouts,
					reqs.MinQuickChecks,
					reqs.MinHeadings,
					reqs.MinDiagrams,
					reqs.MinTables,
					reqs.MinWhyItMatters,
					reqs.MinIntuition,
					reqs.MinMentalModels,
					reqs.MinPitfalls,
					reqs.MinSteps,
					reqs.MinChecklist,
					mediaRequirementLine,
					generatedFigureRequirementLine,
					citationsRequirementLine,
					templateRequirementLine,
					pathStyleJSON,
					suggestedOutline,
					outlineDiagramLine,
					suggestedVideoLine(videoAsset),
					excerpts,
					formatChunkIDBullets(chunkIDs),
					assetsJSON,
					generatedFigures,
				) + feedback

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

				var orderRepairMetrics map[string]any
				gen, orderRepairMetrics = content.RepairNodeDocGenOrder(gen)
				if orderErrs, orderMetrics := content.NodeDocGenOrderIssues(gen); len(orderErrs) > 0 {
					if orderRepairMetrics == nil {
						orderRepairMetrics = map[string]any{}
					}
					orderRepairMetrics["order_issues"] = orderMetrics
					orderRepairMetrics["order_errors"] = orderErrs
				}

				if outlineErrs := outlineHeadingOrderErrors(gen, outline); len(outlineErrs) > 0 {
					lastErrors = append([]string{"outline_mismatch"}, outlineErrs...)
					if deps.GenRuns != nil {
						_, _ = deps.GenRuns.Create(dbctx.Context{Ctx: ctx}, []*types.LearningDocGenerationRun{
							makeGenRun("node_doc", nil, in.OwnerUserID, pathID, w.Node.ID, "failed", nodeDocPromptVersion, attempt, latency, lastErrors, nil),
						})
					}
					continue
				}

				if len(gen.ConceptKeys) == 0 {
					fallback := dedupeStrings(w.ConceptKeys)
					if len(fallback) == 0 {
						for _, sec := range outline.Sections {
							fallback = append(fallback, sec.ConceptKeys...)
						}
						fallback = dedupeStrings(fallback)
					}
					if len(fallback) == 0 {
						fallback = dedupeStrings(w.PrereqKeys)
					}
					if len(fallback) == 0 && strings.TrimSpace(w.ConceptCSV) != "" {
						fallback = dedupeStrings(strings.Split(w.ConceptCSV, ","))
					}
					if len(fallback) == 0 {
						title := strings.TrimSpace(w.Node.Title)
						if title == "" {
							title = "general"
						}
						fallback = []string{title}
					}
					gen.ConceptKeys = fallback
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
				// Best-effort padding for near-miss minima (e.g., missing one paragraph).
				// This prevents hard failures on small structural omissions without weakening validation.
				if !strictNarrative {
					if patched, changed := ensureNodeDocMeetsMinima(doc, reqs, allowedChunkIDs, chunkByID, chunkIDs); changed {
						doc = patched
					}
				}
				if withIDs, changed := content.EnsureNodeDocBlockIDs(doc); changed {
					doc = withIDs
				}
				var polishStats map[string]any
				if envBool("NODE_DOC_POLISH_ENABLED", true) {
					if hits := content.DetectNodeDocMetaPhrases(doc); len(hits) > 0 {
						if polished, stats, ok := polishNodeDocMeta(gctx, deps, doc, styleManifestPromptJSON, pathNarrativePromptJSON, nodeNarrativeJSON); ok {
							doc = polished
							polishStats = stats
							if scrubbed, phrases := content.ScrubNodeDocV1(doc); len(phrases) > 0 {
								doc = scrubbed
							}
							if withIDs, changed := content.EnsureNodeDocBlockIDs(doc); changed {
								doc = withIDs
							}
						}
					}
				}

				if len(availableAssets) > 0 {
					mediaUsedMu.Lock()
					doc, mediaStats := dedupeNodeDocMedia(doc, availableAssets, mediaUsed)
					for _, url := range nodeDocMediaURLs(doc) {
						if url != "" {
							mediaUsed[url] = true
						}
					}
					mediaUsedMu.Unlock()
					if len(mediaStats) > 0 {
						if orderRepairMetrics == nil {
							orderRepairMetrics = map[string]any{}
						}
						orderRepairMetrics["media_dedupe"] = mediaStats
					}
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

				var teachOrderStats quickCheckTeachOrderStats
				teachOrderChanged := false
				if patched, stats, changed := ensureQuickChecksAfterTeaching(doc, chunkByID); changed {
					doc = patched
					teachOrderStats = stats
					teachOrderChanged = true
					if withIDs, changedIDs := content.EnsureNodeDocBlockIDs(doc); changedIDs {
						doc = withIDs
					}
				}
				threadingInjected := false
				if strictNarrative {
					if patched, changed := ensureNodeDocThreadingReferences(doc, prevTitle, nextTitle, moduleTitle); changed {
						doc = patched
						threadingInjected = true
						if withIDs, changedIDs := content.EnsureNodeDocBlockIDs(doc); changedIDs {
							doc = withIDs
						}
					}
				}

				errs, metrics := content.ValidateNodeDocV1(doc, allowedChunkIDs, reqs)
				if len(polishStats) > 0 {
					metrics["meta_polish"] = polishStats
				}
				if len(orderRepairMetrics) > 0 {
					metrics["order_repair"] = orderRepairMetrics
				}
				if citationsSanitized {
					metrics["citations_sanitized"] = true
					metrics["citations_sanitize"] = citationStats.Map()
				}
				if teachOrderChanged {
					metrics["quick_check_teach_order"] = teachOrderStats.Map()
				}
				if threadingInjected {
					metrics["threading_injected"] = true
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
				if strictNarrative {
					docText, _ := metrics["doc_text"].(string)
					threadErrs, threadMetrics := validateNodeDocThreading(docText, prevTitle, nextTitle, moduleTitle)
					if len(threadMetrics) > 0 {
						metrics["threading"] = threadMetrics
					}
					if len(threadErrs) > 0 {
						errs = append(errs, threadErrs...)
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
