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

type NodeVideosPlanBuildDeps struct {
	DB  *gorm.DB
	Log *logger.Logger

	Path      repos.PathRepo
	PathNodes repos.PathNodeRepo
	Videos    repos.LearningNodeVideoRepo
	GenRuns   repos.LearningDocGenerationRunRepo

	Files  repos.MaterialFileRepo
	Chunks repos.MaterialChunkRepo

	AI  openai.Client
	Vec pc.VectorStore

	Bootstrap services.LearningBuildBootstrapService
}

type NodeVideosPlanBuildInput struct {
	OwnerUserID   uuid.UUID
	MaterialSetID uuid.UUID
	SagaID        uuid.UUID
	PathID        uuid.UUID
}

type NodeVideosPlanBuildOutput struct {
	PathID         uuid.UUID `json:"path_id"`
	NodesPlanned   int       `json:"nodes_planned"`
	NodesSkipped   int       `json:"nodes_skipped"`
	VideosPlanned  int       `json:"videos_planned"`
	VideosExisting int       `json:"videos_existing"`
}

const nodeVideoPlanPromptVersion = "video_plan_v2@1"

func NodeVideosPlanBuild(ctx context.Context, deps NodeVideosPlanBuildDeps, in NodeVideosPlanBuildInput) (NodeVideosPlanBuildOutput, error) {
	out := NodeVideosPlanBuildOutput{}
	if deps.DB == nil || deps.Log == nil || deps.Path == nil || deps.PathNodes == nil || deps.Videos == nil || deps.Files == nil || deps.Chunks == nil || deps.AI == nil || deps.Bootstrap == nil {
		return out, fmt.Errorf("node_videos_plan_build: missing deps")
	}
	if in.OwnerUserID == uuid.Nil {
		return out, fmt.Errorf("node_videos_plan_build: missing owner_user_id")
	}
	if in.MaterialSetID == uuid.Nil {
		return out, fmt.Errorf("node_videos_plan_build: missing material_set_id")
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

	// Feature gate: require video model configured, otherwise skip (no-op).
	if strings.TrimSpace(os.Getenv("OPENAI_VIDEO_MODEL")) == "" {
		deps.Log.Warn("OPENAI_VIDEO_MODEL missing; skipping node_videos_plan_build")
		return out, nil
	}
	if envIntAllowZero("NODE_VIDEOS_RENDER_LIMIT", -1) == 0 {
		deps.Log.Warn("NODE_VIDEOS_RENDER_LIMIT=0; skipping node_videos_plan_build")
		return out, nil
	}

	// Safety: don't break legacy installs where migrations haven't created the new tables yet.
	if !deps.DB.Migrator().HasTable(&types.LearningNodeVideo{}) {
		deps.Log.Warn("learning_node_video table missing; skipping node_videos_plan_build (RUN_MIGRATIONS?)")
		return out, nil
	}

	videoPlanSchema, err := schema.VideoPlanV2()
	if err != nil {
		return out, err
	}

	nodes, err := deps.PathNodes.GetByPathIDs(dbctx.Context{Ctx: ctx}, []uuid.UUID{pathID})
	if err != nil {
		return out, err
	}
	if len(nodes) == 0 {
		return out, fmt.Errorf("node_videos_plan_build: no path nodes (run path_plan_build first)")
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Index < nodes[j].Index })

	nodeIDs := make([]uuid.UUID, 0, len(nodes))
	for _, n := range nodes {
		if n != nil && n.ID != uuid.Nil {
			nodeIDs = append(nodeIDs, n.ID)
		}
	}

	existingRows, err := deps.Videos.GetByPathNodeIDs(dbctx.Context{Ctx: ctx}, nodeIDs)
	if err != nil {
		return out, err
	}
	existingByNode := map[uuid.UUID][]*types.LearningNodeVideo{}
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
			deps.Log.Warn("node_videos_plan_build: intake filter excluded all files; ignoring filter", "path_id", pathID.String())
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
		return out, fmt.Errorf("node_videos_plan_build: no chunks for material set")
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
				chunkEmbsErr = fmt.Errorf("node_videos_plan_build: no local embeddings available (run embed_chunks first)")
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
				out.VideosExisting += len(rows)
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
		return out, fmt.Errorf("node_videos_plan_build: embedding count mismatch (got %d want %d)", len(queryEmbs), len(work))
	}
	for i := range work {
		work[i].QueryEmb = queryEmbs[i]
		if len(work[i].QueryEmb) == 0 {
			return out, fmt.Errorf("node_videos_plan_build: empty query embedding")
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

	maxConc := envInt("NODE_VIDEOS_PLAN_CONCURRENCY", 4)
	if maxConc < 1 {
		maxConc = 1
	}

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConc)

	var nodesPlanned int32
	var vidsPlanned int32

	for i := range work {
		w := work[i]
		g.Go(func() error {
			if w.Node == nil || w.Node.ID == uuid.Nil {
				return nil
			}

			// ---- Evidence retrieval (semantic + lexical + fallback) ----
			const semanticK = 18
			const lexicalK = 8
			const finalK = 18

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
				return fmt.Errorf("node_videos_plan_build: empty grounding excerpts")
			}

			// Deterministic "noun-ish" subject candidates to keep video selection grounded in the actual content.
			subjectHints := extractVisualSubjectCandidates(excerpts, 18)

			allowedChunkIDs := map[string]bool{}
			for _, id := range chunkIDs {
				allowedChunkIDs[id.String()] = true
			}

			system := `
MODE: VIDEO_PLANNER

You decide whether a short supplementary video would meaningfully help this node.
Videos should add value beyond diagrams by showing motion, dynamics, spatial intuition, or a realistic setup/demo.

Continuity rules (critical for multi-clip stitching):
- If you output multiple clips, they MUST feel like one continuous coherent video (same visual style, same setting, consistent camera language).
- Avoid random scene changes or style switches between clips. Prefer a single “visual bible” for the entire storyboard.
- Plan clean seams: each clip should start and end with a brief stable hold on a visually clean frame so stitching can crossfade seamlessly.

Hard rules:
- Return ONLY valid JSON matching the schema (no surrounding text).
- Plan 0–1 videos total (max 1).
- Every plan item must include citations referencing ONLY provided chunk_ids.
- Every clip prompt MUST include: no watermarks; no logos; no brand names; avoid identifiable people/faces.
`

			maxClipSec := 12
			maxTotalSec := envIntAllowZero("NODE_VIDEO_MAX_TOTAL_SECONDS", 36)
			maxClips := envIntAllowZero("NODE_VIDEO_MAX_CLIPS_PER_VIDEO", 4)
			if maxTotalSec <= 0 {
				maxTotalSec = 36
			}
			if maxClips <= 0 {
				maxClips = 4
			}

			user := fmt.Sprintf(`
NODE_TITLE: %s
NODE_GOAL: %s
CONCEPT_KEYS: %s

GROUNDING_EXCERPTS (chunk_id lines):
%s

VIDEO_SUBJECT_CANDIDATES (noun/thing hints extracted from the excerpts; prefer concrete, depictable subjects):
%s

Task:
- If a video would add motion/spatial intuition/realistic setup value, output exactly 1 plan.
- Otherwise output videos: [].
- Use storyboard planning: break the idea into a sequence of beats with timings.
- If the full storyboard would exceed the model max clip duration, split into multiple clips and we will stitch them together.
- Model max clip duration: %ds (choose clip.duration_sec from {4, 8, 12} only).
- Total duration should be as short as possible; do not exceed %ds. Do not exceed %d clips.
- Each clip prompt MUST mention at least one subject from VIDEO_SUBJECT_CANDIDATES (verbatim).
- On-screen labels/subtitles are OK when they help; include them explicitly in the beat descriptions and clip prompt.
- Seamlessness requirement (important):
  - Assume clips are stitched with a short crossfade (~0.25s).
  - For every clip: design the FIRST ~0.3s and LAST ~0.3s to be a stable hold on a clean frame (no new text appearing/disappearing during the hold).
  - Clip boundaries must be “matchable”: keep the same scene, palette, and framing across clips; end clip N on the exact visual state that clip N+1 starts from.
  - Keep transitions subtle and consistent; use beat.transition to describe crossfade/match-cut style, not flashy wipes.
`,
				w.Node.Title,
				w.Goal,
				w.ConceptCSV,
				excerpts,
				strings.Join(subjectHints, ", "),
				maxClipSec,
				maxTotalSec,
				maxClips,
			)

			var lastErrs []string
			var plan content.VideoPlanDocV2
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
				obj, genErr := deps.AI.GenerateJSON(gctx, system, user+feedback, "video_plan_v2", videoPlanSchema)
				latency = int(time.Since(start).Milliseconds())

				if genErr != nil {
					lastErrs = []string{"generate_failed: " + genErr.Error()}
					if deps.GenRuns != nil {
						_, _ = deps.GenRuns.Create(dbctx.Context{Ctx: ctx}, []*types.LearningDocGenerationRun{
							makeGenRun("node_video_plan", nil, in.OwnerUserID, pathID, w.Node.ID, "failed", nodeVideoPlanPromptVersion, attempt, latency, lastErrs, nil),
						})
					}
					continue
				}

				raw, _ := json.Marshal(obj)
				var tmp content.VideoPlanDocV2
				if err := json.Unmarshal(raw, &tmp); err != nil {
					lastErrs = []string{"schema_unmarshal_failed"}
					if deps.GenRuns != nil {
						_, _ = deps.GenRuns.Create(dbctx.Context{Ctx: ctx}, []*types.LearningDocGenerationRun{
							makeGenRun("node_video_plan", nil, in.OwnerUserID, pathID, w.Node.ID, "failed", nodeVideoPlanPromptVersion, attempt, latency, lastErrs, nil),
						})
					}
					continue
				}

				errs, qm := content.ValidateVideoPlanV2(tmp, allowedChunkIDs, subjectHints, maxClipSec, maxTotalSec, maxClips)
				metrics = qm
				if len(errs) > 0 {
					lastErrs = errs
					if deps.GenRuns != nil {
						_, _ = deps.GenRuns.Create(dbctx.Context{Ctx: ctx}, []*types.LearningDocGenerationRun{
							makeGenRun("node_video_plan", nil, in.OwnerUserID, pathID, w.Node.ID, "failed", nodeVideoPlanPromptVersion, attempt, latency, errs, qm),
						})
					}
					continue
				}

				plan = tmp
				ok = true
				succAttempt = attempt
				break
			}

			sourcesHash := content.HashSources(nodeVideoPlanPromptVersion, 2, mapKeys(allowedChunkIDs))
			now := time.Now().UTC()

			if !ok {
				// Best-effort stage: persist a sentinel row and continue the pipeline.
				planJSON, _ := json.Marshal(map[string]any{
					"videos": []any{},
					"reason": "planner_failed",
					"errors": lastErrs,
				})
				row := &types.LearningNodeVideo{
					ID:            uuid.New(),
					UserID:        in.OwnerUserID,
					PathID:        pathID,
					PathNodeID:    w.Node.ID,
					Slot:          0,
					SchemaVersion: 2,
					PlanJSON:      datatypes.JSON(planJSON),
					PromptHash:    content.HashBytes([]byte("skipped:" + w.Node.ID.String())),
					SourcesHash:   sourcesHash,
					Status:        "skipped",
					Error:         shorten(strings.Join(lastErrs, " | "), 900),
					CreatedAt:     now,
					UpdatedAt:     now,
				}
				_ = deps.Videos.Upsert(dbctx.Context{Ctx: ctx}, row)
				atomic.AddInt32(&nodesPlanned, 1)
				return nil
			}

			// Persist 0/1 planned rows, or a sentinel "skipped" row to avoid repeated planning.
			if len(plan.Videos) == 0 {
				planJSON, _ := json.Marshal(map[string]any{"videos": []any{}, "reason": "no_videos"})
				row := &types.LearningNodeVideo{
					ID:            uuid.New(),
					UserID:        in.OwnerUserID,
					PathID:        pathID,
					PathNodeID:    w.Node.ID,
					Slot:          0,
					SchemaVersion: 2,
					PlanJSON:      datatypes.JSON(planJSON),
					PromptHash:    content.HashBytes([]byte("skipped:" + w.Node.ID.String())),
					SourcesHash:   sourcesHash,
					Status:        "skipped",
					CreatedAt:     now,
					UpdatedAt:     now,
				}
				_ = deps.Videos.Upsert(dbctx.Context{Ctx: ctx}, row)
				atomic.AddInt32(&nodesPlanned, 1)
			} else {
				for i := range plan.Videos {
					item := plan.Videos[i]
					b, _ := json.Marshal(item)
					ph := strings.TrimSpace(item.Prompt)
					for _, c := range item.Storyboard.Clips {
						if strings.TrimSpace(c.Prompt) != "" {
							ph += "\n\nCLIP:\n" + strings.TrimSpace(c.Prompt)
						}
					}
					row := &types.LearningNodeVideo{
						ID:            uuid.New(),
						UserID:        in.OwnerUserID,
						PathID:        pathID,
						PathNodeID:    w.Node.ID,
						Slot:          i + 1,
						SchemaVersion: 2,
						PlanJSON:      datatypes.JSON(b),
						PromptHash:    content.HashBytes([]byte(ph)),
						SourcesHash:   sourcesHash,
						Status:        "planned",
						CreatedAt:     now,
						UpdatedAt:     now,
					}
					_ = deps.Videos.Upsert(dbctx.Context{Ctx: ctx}, row)
					atomic.AddInt32(&vidsPlanned, 1)
				}
				atomic.AddInt32(&nodesPlanned, 1)
			}

			if deps.GenRuns != nil {
				_, _ = deps.GenRuns.Create(dbctx.Context{Ctx: ctx}, []*types.LearningDocGenerationRun{
					makeGenRun("node_video_plan", nil, in.OwnerUserID, pathID, w.Node.ID, "succeeded", nodeVideoPlanPromptVersion, succAttempt, latency, nil, metrics),
				})
			}

			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return out, err
	}

	out.NodesPlanned = int(atomic.LoadInt32(&nodesPlanned))
	out.VideosPlanned = int(atomic.LoadInt32(&vidsPlanned))

	return out, nil
}
