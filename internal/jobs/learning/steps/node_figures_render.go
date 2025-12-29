package steps

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/clients/gcp"
	"github.com/yungbote/neurobridge-backend/internal/clients/openai"
	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/learning/content"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
	"golang.org/x/sync/errgroup"
)

type NodeFiguresRenderDeps struct {
	DB  *gorm.DB
	Log *logger.Logger

	Path      repos.PathRepo
	PathNodes repos.PathNodeRepo
	Figures   repos.LearningNodeFigureRepo
	Assets    repos.AssetRepo
	GenRuns   repos.LearningDocGenerationRunRepo

	AI     openai.Client
	Bucket gcp.BucketService

	Bootstrap services.LearningBuildBootstrapService
}

type NodeFiguresRenderInput struct {
	OwnerUserID   uuid.UUID
	MaterialSetID uuid.UUID
	SagaID        uuid.UUID
}

type NodeFiguresRenderOutput struct {
	PathID         uuid.UUID `json:"path_id"`
	FiguresRendered int      `json:"figures_rendered"`
	FiguresExisting int      `json:"figures_existing"`
	FiguresFailed   int      `json:"figures_failed"`
}

const nodeFigureAssetPromptVersion = "figure_asset_v1@1"

func NodeFiguresRender(ctx context.Context, deps NodeFiguresRenderDeps, in NodeFiguresRenderInput) (NodeFiguresRenderOutput, error) {
	out := NodeFiguresRenderOutput{}
	if deps.DB == nil || deps.Log == nil || deps.Path == nil || deps.PathNodes == nil || deps.Figures == nil || deps.AI == nil || deps.Bootstrap == nil {
		return out, fmt.Errorf("node_figures_render: missing deps")
	}
	if in.OwnerUserID == uuid.Nil {
		return out, fmt.Errorf("node_figures_render: missing owner_user_id")
	}
	if in.MaterialSetID == uuid.Nil {
		return out, fmt.Errorf("node_figures_render: missing material_set_id")
	}

	pathID, err := deps.Bootstrap.EnsurePath(dbctx.Context{Ctx: ctx}, in.OwnerUserID, in.MaterialSetID)
	if err != nil {
		return out, err
	}
	out.PathID = pathID

	// Feature gate: require image model + bucket configured; otherwise no-op.
	if strings.TrimSpace(os.Getenv("OPENAI_IMAGE_MODEL")) == "" {
		deps.Log.Warn("OPENAI_IMAGE_MODEL missing; skipping node_figures_render")
		return out, nil
	}
	if deps.Bucket == nil {
		deps.Log.Warn("Bucket service missing; skipping node_figures_render")
		return out, nil
	}

	// Safety: don't break legacy installs where migrations haven't created the new tables yet.
	if !deps.DB.Migrator().HasTable(&types.LearningNodeFigure{}) {
		deps.Log.Warn("learning_node_figure table missing; skipping node_figures_render (RUN_MIGRATIONS?)")
		return out, nil
	}

	nodes, err := deps.PathNodes.GetByPathIDs(dbctx.Context{Ctx: ctx}, []uuid.UUID{pathID})
	if err != nil {
		return out, err
	}
	if len(nodes) == 0 {
		return out, fmt.Errorf("node_figures_render: no path nodes (run path_plan_build first)")
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Index < nodes[j].Index })

	nodeIDs := make([]uuid.UUID, 0, len(nodes))
	for _, n := range nodes {
		if n != nil && n.ID != uuid.Nil {
			nodeIDs = append(nodeIDs, n.ID)
		}
	}

	rows, err := deps.Figures.GetByPathNodeIDs(dbctx.Context{Ctx: ctx}, nodeIDs)
	if err != nil {
		return out, err
	}

	work := make([]*types.LearningNodeFigure, 0)
	for _, r := range rows {
		if r == nil || r.PathNodeID == uuid.Nil {
			continue
		}
		if r.Slot <= 0 {
			continue
		}
		status := strings.ToLower(strings.TrimSpace(r.Status))
		if status == "rendered" && strings.TrimSpace(r.AssetURL) != "" {
			out.FiguresExisting++
			continue
		}
		if status != "planned" && status != "rendered" {
			continue
		}
		if strings.TrimSpace(r.AssetURL) != "" {
			out.FiguresExisting++
			continue
		}
		work = append(work, r)
	}
	if len(work) == 0 {
		return out, nil
	}

	maxConc := envInt("NODE_FIGURES_RENDER_CONCURRENCY", 2)
	if maxConc < 1 {
		maxConc = 1
	}
	if maxConc > 4 {
		maxConc = 4
	}

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConc)

	var rendered int32
	var failed int32

	for i := range work {
		row := work[i]
		g.Go(func() error {
			if row == nil || row.PathNodeID == uuid.Nil {
				return nil
			}

			start := time.Now()

			var plan content.FigurePlanItemV1
			if len(row.PlanJSON) == 0 || strings.TrimSpace(string(row.PlanJSON)) == "" || string(row.PlanJSON) == "null" {
				return markFigureFailed(gctx, deps, row, "missing_plan_json", 0)
			}
			if err := json.Unmarshal(row.PlanJSON, &plan); err != nil {
				return markFigureFailed(gctx, deps, row, "invalid_plan_json", 0)
			}

			prompt := strings.TrimSpace(plan.Prompt)
			if prompt == "" {
				return markFigureFailed(gctx, deps, row, "empty_prompt", 0)
			}

			// Guardrails: reinforce that figures are photorealistic, not diagram-like, and must avoid text/logos.
			// We append (rather than replace) to preserve the planner's domain-specific content.
			constraints := "Hard constraints: photorealistic / high-resolution / realistic lighting (or high-fidelity 3D render); NOT a diagram/schematic/infographic; NO arrows/callouts; NO text or labels in the image; no watermarks; no logos; no brand names; avoid identifiable people/faces."
			prompt = prompt + "\n\n" + constraints

			img, err := deps.AI.GenerateImage(gctx, prompt)
			latency := int(time.Since(start).Milliseconds())
			if err != nil {
				_ = markFigureFailed(gctx, deps, row, "image_generate_failed: "+err.Error(), latency)
				atomic.AddInt32(&failed, 1)
				return nil
			}
			if len(img.Bytes) == 0 {
				_ = markFigureFailed(gctx, deps, row, "image_generate_empty", latency)
				atomic.AddInt32(&failed, 1)
				return nil
			}

			storageKey := fmt.Sprintf("generated/node_figures/%s/%s/slot_%d_%s.png",
				pathID.String(),
				row.PathNodeID.String(),
				row.Slot,
				strings.TrimSpace(row.PromptHash),
			)
			if err := deps.Bucket.UploadFile(gdbctx.Context{Ctx: ctx}, gcp.BucketCategoryMaterial, storageKey, bytes.NewReader(img.Bytes)); err != nil {
				_ = markFigureFailed(gctx, deps, row, "upload_failed: "+err.Error(), latency)
				atomic.AddInt32(&failed, 1)
				return nil
			}

			publicURL := deps.Bucket.GetPublicURL(gcp.BucketCategoryMaterial, storageKey)
			mime := strings.TrimSpace(row.AssetMimeType)
			if mime == "" {
				mime = strings.TrimSpace(img.MimeType)
			}
			if mime == "" {
				mime = "image/png"
			}

			var assetID *uuid.UUID
			if deps.Assets != nil {
				meta := map[string]any{
					"asset_kind":      "generated_figure",
					"semantic_type":   strings.TrimSpace(plan.SemanticType),
					"caption":         strings.TrimSpace(plan.Caption),
					"alt_text":        strings.TrimSpace(plan.AltText),
					"placement_hint":  strings.TrimSpace(plan.PlacementHint),
					"citations":       content.NormalizeConceptKeys(plan.Citations), // stable-ish string slice cleanup
					"prompt_hash":     strings.TrimSpace(row.PromptHash),
					"sources_hash":    strings.TrimSpace(row.SourcesHash),
					"revised_prompt":  strings.TrimSpace(img.RevisedPrompt),
				}
				b, _ := json.Marshal(meta)
				aid := uuid.New()
				a := &types.Asset{
					ID:         aid,
					Kind:       "image",
					StorageKey: storageKey,
					URL:        publicURL,
					OwnerType:  "learning_node_figure",
					OwnerID:    row.ID,
					Metadata:   datatypes.JSON(b),
					CreatedAt:  time.Now().UTC(),
					UpdatedAt:  time.Now().UTC(),
				}
				if _, err := deps.Assets.Create(gdbctx.Context{Ctx: ctx}, []*types.Asset{a}); err == nil {
					assetID = &aid
				}
			}

			now := time.Now().UTC()
			update := &types.LearningNodeFigure{
				ID:              row.ID,
				UserID:          row.UserID,
				PathID:          row.PathID,
				PathNodeID:      row.PathNodeID,
				Slot:            row.Slot,
				SchemaVersion:   row.SchemaVersion,
				PlanJSON:        row.PlanJSON,
				PromptHash:      row.PromptHash,
				SourcesHash:     row.SourcesHash,
				Status:          "rendered",
				AssetID:         assetID,
				AssetStorageKey: storageKey,
				AssetURL:        publicURL,
				AssetMimeType:   mime,
				Error:           "",
				CreatedAt:       row.CreatedAt,
				UpdatedAt:       now,
			}
			_ = deps.Figures.Upsert(gdbctx.Context{Ctx: ctx}, update)

			if deps.GenRuns != nil {
				metrics := map[string]any{
					"storage_key": storageKey,
					"url":         publicURL,
					"byte_len":    len(img.Bytes),
				}
				_, _ = deps.GenRuns.Create(gdbctx.Context{Ctx: ctx}, []*types.LearningDocGenerationRun{
					makeGenRun("node_figure_asset", &update.ID, in.OwnerUserID, pathID, row.PathNodeID, "succeeded", nodeFigureAssetPromptVersion, 1, latency, nil, metrics),
				})
			}

			atomic.AddInt32(&rendered, 1)
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return out, err
	}

	out.FiguresRendered = int(atomic.LoadInt32(&rendered))
	out.FiguresFailed = int(atomic.LoadInt32(&failed))

	return out, nil
}

func markFigureFailed(ctx context.Context, deps NodeFiguresRenderDeps, row *types.LearningNodeFigure, errMsg string, latencyMS int) error {
	if row == nil || deps.Figures == nil {
		return nil
	}
	errMsg = strings.TrimSpace(errMsg)
	if len(errMsg) > 900 {
		errMsg = errMsg[:900]
	}
	now := time.Now().UTC()
	update := &types.LearningNodeFigure{
		ID:              row.ID,
		UserID:          row.UserID,
		PathID:          row.PathID,
		PathNodeID:      row.PathNodeID,
		Slot:            row.Slot,
		SchemaVersion:   row.SchemaVersion,
		PlanJSON:        row.PlanJSON,
		PromptHash:      row.PromptHash,
		SourcesHash:     row.SourcesHash,
		Status:          "failed",
		AssetID:         row.AssetID,
		AssetStorageKey: row.AssetStorageKey,
		AssetURL:        row.AssetURL,
		AssetMimeType:   row.AssetMimeType,
		Error:           errMsg,
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       now,
	}
	_ = deps.Figures.Upsert(dbctx.Context{Ctx: ctx}, update)

	if deps.GenRuns != nil {
		_, _ = deps.GenRuns.Create(dbctx.Context{Ctx: ctx}, []*types.LearningDocGenerationRun{
			makeGenRun("node_figure_asset", &update.ID, row.UserID, row.PathID, row.PathNodeID, "failed", nodeFigureAssetPromptVersion, 1, latencyMS, []string{errMsg}, nil),
		})
	}
	return nil
}
