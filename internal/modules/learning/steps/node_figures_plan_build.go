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

	"github.com/yungbote/neurobridge-backend/internal/data/materialsetctx"
	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/content"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/content/schema"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/index"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
	pc "github.com/yungbote/neurobridge-backend/internal/platform/pinecone"
	"github.com/yungbote/neurobridge-backend/internal/services"
	"golang.org/x/sync/errgroup"
)

type NodeFiguresPlanBuildDeps struct {
	DB  *gorm.DB
	Log *logger.Logger

	Path      repos.PathRepo
	PathNodes repos.PathNodeRepo
	Figures   repos.LearningNodeFigureRepo
	GenRuns   repos.LearningDocGenerationRunRepo

	Files  repos.MaterialFileRepo
	Chunks repos.MaterialChunkRepo

	AI  openai.Client
	Vec pc.VectorStore

	Bootstrap services.LearningBuildBootstrapService
}

type NodeFiguresPlanBuildInput struct {
	OwnerUserID   uuid.UUID
	MaterialSetID uuid.UUID
	SagaID        uuid.UUID
	PathID        uuid.UUID
}

type NodeFiguresPlanBuildOutput struct {
	PathID          uuid.UUID `json:"path_id"`
	NodesPlanned    int       `json:"nodes_planned"`
	NodesSkipped    int       `json:"nodes_skipped"`
	FiguresPlanned  int       `json:"figures_planned"`
	FiguresExisting int       `json:"figures_existing"`
}

const nodeFigurePlanPromptVersion = "figure_plan_v1@1"

func NodeFiguresPlanBuild(ctx context.Context, deps NodeFiguresPlanBuildDeps, in NodeFiguresPlanBuildInput) (NodeFiguresPlanBuildOutput, error) {
	out := NodeFiguresPlanBuildOutput{}
	if deps.DB == nil || deps.Log == nil || deps.Path == nil || deps.PathNodes == nil || deps.Figures == nil || deps.Files == nil || deps.Chunks == nil || deps.AI == nil || deps.Bootstrap == nil {
		return out, fmt.Errorf("node_figures_plan_build: missing deps")
	}
	if in.OwnerUserID == uuid.Nil {
		return out, fmt.Errorf("node_figures_plan_build: missing owner_user_id")
	}
	if in.MaterialSetID == uuid.Nil {
		return out, fmt.Errorf("node_figures_plan_build: missing material_set_id")
	}

	pathID, err := resolvePathID(ctx, deps.Bootstrap, in.OwnerUserID, in.MaterialSetID, in.PathID)
	if err != nil {
		return out, err
	}
	out.PathID = pathID

	// Optional: apply intake material allowlist (noise filtering / multi-material alignment).
	var allowFiles map[uuid.UUID]bool
	if deps.Path != nil {
		if row, err := deps.Path.GetByID(dbctx.Context{Ctx: ctx}, pathID); err == nil && row != nil && len(row.Metadata) > 0 && string(row.Metadata) != "null" {
			var meta map[string]any
			if json.Unmarshal(row.Metadata, &meta) == nil {
				allowFiles = intakeMaterialAllowlistFromPathMeta(meta)
			}
		}
	}

	// Feature gate: require image model configured, otherwise skip (no-op).
	if strings.TrimSpace(os.Getenv("OPENAI_IMAGE_MODEL")) == "" {
		deps.Log.Warn("OPENAI_IMAGE_MODEL missing; skipping node_figures_plan_build")
		return out, nil
	}
	if envIntAllowZero("NODE_FIGURES_RENDER_LIMIT", -1) == 0 {
		deps.Log.Warn("NODE_FIGURES_RENDER_LIMIT=0; skipping node_figures_plan_build")
		return out, nil
	}

	// Safety: don't break legacy installs where migrations haven't created the new tables yet.
	if !deps.DB.Migrator().HasTable(&types.LearningNodeFigure{}) {
		deps.Log.Warn("learning_node_figure table missing; skipping node_figures_plan_build (RUN_MIGRATIONS?)")
		return out, nil
	}

	figPlanSchema, err := schema.FigurePlanV1()
	if err != nil {
		return out, err
	}

	nodes, err := deps.PathNodes.GetByPathIDs(dbctx.Context{Ctx: ctx}, []uuid.UUID{pathID})
	if err != nil {
		return out, err
	}
	if len(nodes) == 0 {
		return out, fmt.Errorf("node_figures_plan_build: no path nodes (run path_plan_build first)")
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Index < nodes[j].Index })

	nodeIDs := make([]uuid.UUID, 0, len(nodes))
	for _, n := range nodes {
		if n != nil && n.ID != uuid.Nil {
			nodeIDs = append(nodeIDs, n.ID)
		}
	}

	existingRows, err := deps.Figures.GetByPathNodeIDs(dbctx.Context{Ctx: ctx}, nodeIDs)
	if err != nil {
		return out, err
	}
	existingByNode := map[uuid.UUID][]*types.LearningNodeFigure{}
	for _, r := range existingRows {
		if r == nil || r.PathNodeID == uuid.Nil {
			continue
		}
		existingByNode[r.PathNodeID] = append(existingByNode[r.PathNodeID], r)
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
			deps.Log.Warn("node_figures_plan_build: intake filter excluded all files; ignoring filter", "path_id", pathID.String())
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
		return out, fmt.Errorf("node_figures_plan_build: no chunks for material set")
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

	// Lazy deterministic scan order for cosine fallback.
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
				chunkEmbsErr = fmt.Errorf("node_figures_plan_build: no local embeddings available (run embed_chunks first)")
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
		// Skip if already planned/rendered (only re-plan if all rows are failed).
		if rows := existingByNode[node.ID]; len(rows) > 0 {
			allFailed := true
			for _, r := range rows {
				if r != nil && !strings.EqualFold(strings.TrimSpace(r.Status), "failed") {
					allFailed = false
					break
				}
			}
			if !allFailed {
				out.FiguresExisting += len(rows)
				out.NodesSkipped++
				continue
			}
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

	// Batch query embeddings.
	queryTexts := make([]string, 0, len(work))
	for _, w := range work {
		queryTexts = append(queryTexts, w.QueryText)
	}
	queryEmbs, err := deps.AI.Embed(ctx, queryTexts)
	if err != nil {
		return out, err
	}
	if len(queryEmbs) != len(work) {
		return out, fmt.Errorf("node_figures_plan_build: embedding count mismatch (got %d want %d)", len(queryEmbs), len(work))
	}
	for i := range work {
		work[i].QueryEmb = queryEmbs[i]
		if len(work[i].QueryEmb) == 0 {
			return out, fmt.Errorf("node_figures_plan_build: empty query embedding")
		}
	}

	// Derived material sets share the chunk namespace (and KG products) with their source upload batch.
	sourceSetID := in.MaterialSetID
	if deps.DB != nil {
		if sc, err := materialsetctx.Resolve(ctx, deps.DB, in.MaterialSetID); err == nil && sc.SourceMaterialSetID != uuid.Nil {
			sourceSetID = sc.SourceMaterialSetID
		}
	}
	chunksNS := index.ChunksNamespace(sourceSetID)

	maxConc := envInt("NODE_FIGURES_PLAN_CONCURRENCY", 4)
	if maxConc < 1 {
		maxConc = 1
	}

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConc)

	var nodesPlanned int32
	var figsPlanned int32

	for i := range work {
		w := work[i]
		g.Go(func() error {
			if w.Node == nil || w.Node.ID == uuid.Nil {
				return nil
			}

			// Evidence retrieval (keep small; planner only needs a few grounded cues).
			const semanticK = 12
			const lexicalK = 6
			const finalK = 14

			chunkIDs, _, _ := graphAssistedChunkIDs(gctx, deps.DB, deps.Vec, chunkRetrievePlan{
				MaterialSetID: sourceSetID,
				ChunksNS:      chunksNS,
				QueryText:     w.QueryText,
				QueryEmb:      w.QueryEmb,
				FileIDs:       fileIDs,
				AllowFiles:    allowFiles,
				SeedK:         semanticK,
				LexicalK:      lexicalK,
				FinalK:        finalK,
			})
			chunkIDs = dedupeUUIDsPreserveOrder(chunkIDs)

			if len(chunkIDs) < finalK {
				ce, err := buildChunkEmbs()
				if err != nil {
					return err
				}
				fallback := topKChunkIDsByCosine(w.QueryEmb, ce, finalK)
				chunkIDs = dedupeUUIDsPreserveOrder(append(chunkIDs, fallback...))
			}
			if len(chunkIDs) > finalK {
				chunkIDs = chunkIDs[:finalK]
			}

			excerpts := buildActivityExcerpts(chunkByID, chunkIDs, 12, 650)
			if strings.TrimSpace(excerpts) == "" {
				return fmt.Errorf("node_figures_plan_build: empty grounding excerpts")
			}

			// Deterministic "noun-ish" subject candidates to keep figure selection grounded in the actual content.
			// These are extracted from the retrieved excerpts and used as hints (not hard requirements) for the planner.
			subjectHints := extractVisualSubjectCandidates(excerpts, 18)

			allowedChunkIDs := map[string]bool{}
			for _, id := range chunkIDs {
				allowedChunkIDs[id.String()] = true
			}

			system := `
MODE: FIGURE_PLANNER

You decide whether a raster "figure" (image) would meaningfully help this node.
Figures can be photos, illustrations, or diagrams; choose the style that best conveys the concept.
Labels/equations are OK when they help; include them explicitly in the prompt if needed.

Hard rules:
- Return ONLY valid JSON matching the schema (no surrounding text).
- Plan 0–2 figures total (max 2).
- Every plan item must include citations referencing ONLY provided chunk_ids.
- Image prompt MUST include: no watermarks; no logos; no brand names; avoid identifiable people/faces.
`

			user := fmt.Sprintf(`
NODE_TITLE: %s
NODE_GOAL: %s
CONCEPT_KEYS: %s

GROUNDING_EXCERPTS (chunk_id lines):
%s

VISUAL_SUBJECT_CANDIDATES (noun/thing hints extracted from the excerpts; prefer concrete, depictable subjects):
%s

Task:
- If a figure would reduce abstraction (physical setup/spatial/real-world anchor), output 1–2 plans.
- Otherwise output figures: [].
- Make the prompt specific about what to depict, but keep it faithful to the excerpts.
- Each figure prompt/caption MUST mention at least one subject from VISUAL_SUBJECT_CANDIDATES (verbatim).
`,
				w.Node.Title,
				w.Goal,
				w.ConceptCSV,
				excerpts,
				strings.Join(subjectHints, ", "),
			)

			var lastErrs []string
			var plan content.FigurePlanDocV1
			var metrics map[string]any
			var latency int
			succAttempt := 0
			ok := false

			for attempt := 1; attempt <= 3; attempt++ {
				feedback := ""
				if len(lastErrs) > 0 {
					feedback = "\n\nVALIDATION_ERRORS_TO_FIX:\n- " + strings.Join(lastErrs, "\n- ")
				}

				start := time.Now()
				obj, genErr := deps.AI.GenerateJSON(gctx, system, user+feedback, "figure_plan_v1", figPlanSchema)
				latency = int(time.Since(start).Milliseconds())

				if genErr != nil {
					lastErrs = []string{"generate_failed: " + genErr.Error()}
					if deps.GenRuns != nil {
						_, _ = deps.GenRuns.Create(dbctx.Context{Ctx: ctx}, []*types.LearningDocGenerationRun{
							makeGenRun("node_figure_plan", nil, in.OwnerUserID, pathID, w.Node.ID, "failed", nodeFigurePlanPromptVersion, attempt, latency, lastErrs, nil),
						})
					}
					continue
				}

				raw, _ := json.Marshal(obj)
				var tmp content.FigurePlanDocV1
				if err := json.Unmarshal(raw, &tmp); err != nil {
					lastErrs = []string{"schema_unmarshal_failed"}
					if deps.GenRuns != nil {
						_, _ = deps.GenRuns.Create(dbctx.Context{Ctx: ctx}, []*types.LearningDocGenerationRun{
							makeGenRun("node_figure_plan", nil, in.OwnerUserID, pathID, w.Node.ID, "failed", nodeFigurePlanPromptVersion, attempt, latency, lastErrs, nil),
						})
					}
					continue
				}

				errs, qm := content.ValidateFigurePlanV1(tmp, allowedChunkIDs, subjectHints)
				metrics = qm
				if len(errs) > 0 {
					lastErrs = errs
					if deps.GenRuns != nil {
						_, _ = deps.GenRuns.Create(dbctx.Context{Ctx: ctx}, []*types.LearningDocGenerationRun{
							makeGenRun("node_figure_plan", nil, in.OwnerUserID, pathID, w.Node.ID, "failed", nodeFigurePlanPromptVersion, attempt, latency, errs, qm),
						})
					}
					continue
				}

				plan = tmp
				ok = true
				succAttempt = attempt
				break
			}

			if !ok {
				// Best-effort stage: persist a sentinel row and continue the pipeline.
				planJSON, _ := json.Marshal(map[string]any{
					"figures": []any{},
					"reason":  "planner_failed",
					"errors":  lastErrs,
				})
				now := time.Now().UTC()
				sourcesHash := content.HashSources(nodeFigurePlanPromptVersion, 1, mapKeys(allowedChunkIDs))
				row := &types.LearningNodeFigure{
					ID:            uuid.New(),
					UserID:        in.OwnerUserID,
					PathID:        pathID,
					PathNodeID:    w.Node.ID,
					Slot:          0,
					SchemaVersion: 1,
					PlanJSON:      datatypes.JSON(planJSON),
					PromptHash:    content.HashBytes([]byte("skipped:" + w.Node.ID.String())),
					SourcesHash:   sourcesHash,
					Status:        "skipped",
					Error:         shorten(strings.Join(lastErrs, " | "), 900),
					CreatedAt:     now,
					UpdatedAt:     now,
				}
				_ = deps.Figures.Upsert(dbctx.Context{Ctx: ctx}, row)
				atomic.AddInt32(&nodesPlanned, 1)
				return nil
			}

			sourcesHash := content.HashSources(nodeFigurePlanPromptVersion, 1, mapKeys(allowedChunkIDs))
			now := time.Now().UTC()

			// Persist 1-2 planned rows, or a sentinel "skipped" row to avoid repeated planning.
			if len(plan.Figures) == 0 {
				planJSON, _ := json.Marshal(map[string]any{"figures": []any{}, "reason": "no_figures"})
				row := &types.LearningNodeFigure{
					ID:            uuid.New(),
					UserID:        in.OwnerUserID,
					PathID:        pathID,
					PathNodeID:    w.Node.ID,
					Slot:          0,
					SchemaVersion: 1,
					PlanJSON:      datatypes.JSON(planJSON),
					PromptHash:    content.HashBytes([]byte("skipped:" + w.Node.ID.String())),
					SourcesHash:   sourcesHash,
					Status:        "skipped",
					CreatedAt:     now,
					UpdatedAt:     now,
				}
				_ = deps.Figures.Upsert(dbctx.Context{Ctx: ctx}, row)
				atomic.AddInt32(&nodesPlanned, 1)
			} else {
				for i := range plan.Figures {
					item := plan.Figures[i]
					b, _ := json.Marshal(item)
					row := &types.LearningNodeFigure{
						ID:            uuid.New(),
						UserID:        in.OwnerUserID,
						PathID:        pathID,
						PathNodeID:    w.Node.ID,
						Slot:          i + 1,
						SchemaVersion: 1,
						PlanJSON:      datatypes.JSON(b),
						PromptHash:    content.HashBytes([]byte(strings.TrimSpace(item.Prompt))),
						SourcesHash:   sourcesHash,
						Status:        "planned",
						CreatedAt:     now,
						UpdatedAt:     now,
					}
					_ = deps.Figures.Upsert(dbctx.Context{Ctx: ctx}, row)
					atomic.AddInt32(&figsPlanned, 1)
				}
				atomic.AddInt32(&nodesPlanned, 1)
			}

			if deps.GenRuns != nil {
				_, _ = deps.GenRuns.Create(dbctx.Context{Ctx: ctx}, []*types.LearningDocGenerationRun{
					makeGenRun("node_figure_plan", nil, in.OwnerUserID, pathID, w.Node.ID, "succeeded", nodeFigurePlanPromptVersion, succAttempt, latency, nil, metrics),
				})
			}

			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return out, err
	}

	out.NodesPlanned = int(atomic.LoadInt32(&nodesPlanned))
	out.FiguresPlanned = int(atomic.LoadInt32(&figsPlanned))

	return out, nil
}

func extractVisualSubjectCandidates(excerpts string, max int) []string {
	if max <= 0 {
		max = 12
	}
	s := strings.ToLower(strings.TrimSpace(excerpts))
	if s == "" {
		return nil
	}

	// Strip chunk_id prefixes like "[chunk_id=...]" to reduce noise.
	// We keep the rest of the line content intact.
	for {
		start := strings.Index(s, "[chunk_id=")
		if start < 0 {
			break
		}
		end := strings.Index(s[start:], "]")
		if end < 0 {
			break
		}
		end = start + end + 1
		s = s[:start] + " " + s[end:]
	}

	// Replace non-letters with spaces (simple tokenizer).
	buf := make([]rune, 0, len(s))
	for _, r := range s {
		if r >= 'a' && r <= 'z' {
			buf = append(buf, r)
			continue
		}
		if r == ' ' || r == '\n' || r == '\t' || r == '\r' {
			buf = append(buf, ' ')
			continue
		}
		buf = append(buf, ' ')
	}
	toks := strings.Fields(string(buf))
	if len(toks) == 0 {
		return nil
	}

	stop := map[string]bool{
		"the": true, "and": true, "that": true, "this": true, "with": true, "from": true, "into": true, "over": true, "under": true,
		"your": true, "you": true, "their": true, "they": true, "them": true, "these": true, "those": true,
		"for": true, "are": true, "was": true, "were": true, "have": true, "has": true, "had": true, "will": true, "would": true, "could": true, "should": true,
		"what": true, "when": true, "where": true, "why": true, "how": true, "does": true, "did": true, "doing": true,
		"not": true, "but": true, "also": true, "can": true, "cannot": true, "may": true, "might": true, "must": true,
		"then": true, "than": true, "there": true, "here": true, "only": true, "just": true,
		"lesson": true, "unit": true, "chapter": true, "section": true, "slide": true, "slides": true, "watch": true,
	}

	freq := map[string]int{}
	kept := make([]string, 0, len(toks))
	for _, t := range toks {
		if len(t) < 4 || len(t) > 32 {
			continue
		}
		if stop[t] {
			continue
		}
		freq[t]++
		kept = append(kept, t)
	}

	// Add a small number of common bigrams (helps "spring scale", "free body", etc).
	bigramFreq := map[string]int{}
	for i := 0; i+1 < len(kept); i++ {
		a := kept[i]
		b := kept[i+1]
		if stop[a] || stop[b] {
			continue
		}
		bg := a + " " + b
		bigramFreq[bg]++
	}

	type kv struct {
		K string
		N int
	}

	bigrams := make([]kv, 0, len(bigramFreq))
	for k, n := range bigramFreq {
		if n < 2 {
			continue
		}
		bigrams = append(bigrams, kv{K: k, N: n})
	}
	sort.Slice(bigrams, func(i, j int) bool {
		if bigrams[i].N == bigrams[j].N {
			return bigrams[i].K < bigrams[j].K
		}
		return bigrams[i].N > bigrams[j].N
	})

	unigrams := make([]kv, 0, len(freq))
	for k, n := range freq {
		unigrams = append(unigrams, kv{K: k, N: n})
	}
	sort.Slice(unigrams, func(i, j int) bool {
		if unigrams[i].N == unigrams[j].N {
			return unigrams[i].K < unigrams[j].K
		}
		return unigrams[i].N > unigrams[j].N
	})

	out := make([]string, 0, max)
	seen := map[string]bool{}

	for _, x := range bigrams {
		if len(out) >= max {
			break
		}
		if strings.TrimSpace(x.K) == "" || seen[x.K] {
			continue
		}
		seen[x.K] = true
		out = append(out, x.K)
	}
	for _, x := range unigrams {
		if len(out) >= max {
			break
		}
		if strings.TrimSpace(x.K) == "" || seen[x.K] {
			continue
		}
		seen[x.K] = true
		out = append(out, x.K)
	}

	return out
}
