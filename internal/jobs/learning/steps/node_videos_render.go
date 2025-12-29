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

type NodeVideosRenderDeps struct {
	DB  *gorm.DB
	Log *logger.Logger

	Path      repos.PathRepo
	PathNodes repos.PathNodeRepo
	Videos    repos.LearningNodeVideoRepo
	Assets    repos.AssetRepo
	GenRuns   repos.LearningDocGenerationRunRepo

	AI     openai.Client
	Bucket gcp.BucketService

	Bootstrap services.LearningBuildBootstrapService
}

type NodeVideosRenderInput struct {
	OwnerUserID   uuid.UUID
	MaterialSetID uuid.UUID
	SagaID        uuid.UUID
}

type NodeVideosRenderOutput struct {
	PathID         uuid.UUID `json:"path_id"`
	VideosRendered int       `json:"videos_rendered"`
	VideosExisting int       `json:"videos_existing"`
	VideosFailed   int       `json:"videos_failed"`
}

const nodeVideoAssetPromptVersion = "video_asset_v1@1"

func NodeVideosRender(ctx context.Context, deps NodeVideosRenderDeps, in NodeVideosRenderInput) (NodeVideosRenderOutput, error) {
	out := NodeVideosRenderOutput{}
	if deps.DB == nil || deps.Log == nil || deps.Path == nil || deps.PathNodes == nil || deps.Videos == nil || deps.AI == nil || deps.Bootstrap == nil {
		return out, fmt.Errorf("node_videos_render: missing deps")
	}
	if in.OwnerUserID == uuid.Nil {
		return out, fmt.Errorf("node_videos_render: missing owner_user_id")
	}
	if in.MaterialSetID == uuid.Nil {
		return out, fmt.Errorf("node_videos_render: missing material_set_id")
	}

	pathID, err := deps.Bootstrap.EnsurePath(dbctx.Context{Ctx: ctx}, in.OwnerUserID, in.MaterialSetID)
	if err != nil {
		return out, err
	}
	out.PathID = pathID

	// Feature gate: require video model + bucket configured; otherwise no-op.
	if strings.TrimSpace(os.Getenv("OPENAI_VIDEO_MODEL")) == "" {
		deps.Log.Warn("OPENAI_VIDEO_MODEL missing; skipping node_videos_render")
		return out, nil
	}
	if deps.Bucket == nil {
		deps.Log.Warn("Bucket service missing; skipping node_videos_render")
		return out, nil
	}

	// Safety: don't break legacy installs where migrations haven't created the new tables yet.
	if !deps.DB.Migrator().HasTable(&types.LearningNodeVideo{}) {
		deps.Log.Warn("learning_node_video table missing; skipping node_videos_render (RUN_MIGRATIONS?)")
		return out, nil
	}

	nodes, err := deps.PathNodes.GetByPathIDs(dbctx.Context{Ctx: ctx}, []uuid.UUID{pathID})
	if err != nil {
		return out, err
	}
	if len(nodes) == 0 {
		return out, fmt.Errorf("node_videos_render: no path nodes (run path_plan_build first)")
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Index < nodes[j].Index })

	nodeIDs := make([]uuid.UUID, 0, len(nodes))
	for _, n := range nodes {
		if n != nil && n.ID != uuid.Nil {
			nodeIDs = append(nodeIDs, n.ID)
		}
	}

	rows, err := deps.Videos.GetByPathNodeIDs(dbctx.Context{Ctx: ctx}, nodeIDs)
	if err != nil {
		return out, err
	}

	work := make([]*types.LearningNodeVideo, 0)
	for _, r := range rows {
		if r == nil || r.PathNodeID == uuid.Nil {
			continue
		}
		if r.Slot <= 0 {
			continue
		}
		status := strings.ToLower(strings.TrimSpace(r.Status))
		if status == "rendered" && strings.TrimSpace(r.AssetURL) != "" {
			out.VideosExisting++
			continue
		}
		if status != "planned" && status != "rendered" {
			continue
		}
		if strings.TrimSpace(r.AssetURL) != "" {
			out.VideosExisting++
			continue
		}
		work = append(work, r)
	}
	if len(work) == 0 {
		return out, nil
	}

	maxConc := envInt("NODE_VIDEOS_RENDER_CONCURRENCY", 1)
	if maxConc < 1 {
		maxConc = 1
	}
	if maxConc > 2 {
		maxConc = 2
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

			var plan content.VideoPlanItemV1
			if len(row.PlanJSON) == 0 || strings.TrimSpace(string(row.PlanJSON)) == "" || string(row.PlanJSON) == "null" {
				return markVideoFailed(gctx, deps, row, "missing_plan_json", 0)
			}
			if err := json.Unmarshal(row.PlanJSON, &plan); err != nil {
				return markVideoFailed(gctx, deps, row, "invalid_plan_json", 0)
			}

			prompt := strings.TrimSpace(plan.Prompt)
			if prompt == "" {
				return markVideoFailed(gctx, deps, row, "empty_prompt", 0)
			}

			dur := plan.DurationSec
			if dur <= 0 {
				dur = 8
			}

			// Guardrails: keep videos clean and learner-safe (no text overlays, no branding).
			constraints := "Hard constraints: short educational video; NO text / NO subtitles / NO captions; no watermarks; no logos; no brand names; avoid identifiable people/faces; keep it faithful to the described concept."
			prompt = prompt + "\n\n" + constraints

			vid, err := deps.AI.GenerateVideo(gctx, prompt, openai.VideoGenerationOptions{DurationSeconds: dur})
			latency := int(time.Since(start).Milliseconds())
			if err != nil {
				_ = markVideoFailed(gctx, deps, row, "video_generate_failed: "+err.Error(), latency)
				atomic.AddInt32(&failed, 1)
				return nil
			}
			if len(vid.Bytes) == 0 {
				_ = markVideoFailed(gctx, deps, row, "video_generate_empty", latency)
				atomic.AddInt32(&failed, 1)
				return nil
			}

			mime := strings.TrimSpace(vid.MimeType)
			ext := "mp4"
			switch {
			case strings.Contains(strings.ToLower(mime), "webm"):
				ext = "webm"
			case strings.Contains(strings.ToLower(mime), "mp4"):
				ext = "mp4"
			}

			storageKey := fmt.Sprintf("generated/node_videos/%s/%s/slot_%d_%s.%s",
				pathID.String(),
				row.PathNodeID.String(),
				row.Slot,
				strings.TrimSpace(row.PromptHash),
				ext,
			)
			if err := deps.Bucket.UploadFile(dbctx.Context{Ctx: ctx}, gcp.BucketCategoryMaterial, storageKey, bytes.NewReader(vid.Bytes)); err != nil {
				_ = markVideoFailed(gctx, deps, row, "upload_failed: "+err.Error(), latency)
				atomic.AddInt32(&failed, 1)
				return nil
			}

			publicURL := deps.Bucket.GetPublicURL(gcp.BucketCategoryMaterial, storageKey)
			if mime == "" {
				if ext == "webm" {
					mime = "video/webm"
				} else {
					mime = "video/mp4"
				}
			}

			var assetID *uuid.UUID
			if deps.Assets != nil {
				meta := map[string]any{
					"asset_kind":     "generated_video",
					"semantic_type":  strings.TrimSpace(plan.SemanticType),
					"caption":        strings.TrimSpace(plan.Caption),
					"alt_text":       strings.TrimSpace(plan.AltText),
					"placement_hint": strings.TrimSpace(plan.PlacementHint),
					"citations":      content.NormalizeConceptKeys(plan.Citations),
					"prompt_hash":    strings.TrimSpace(row.PromptHash),
					"sources_hash":   strings.TrimSpace(row.SourcesHash),
					"duration_sec":   dur,
					"revised_prompt": strings.TrimSpace(vid.RevisedPrompt),
				}
				b, _ := json.Marshal(meta)
				aid := uuid.New()
				a := &types.Asset{
					ID:         aid,
					Kind:       "video",
					StorageKey: storageKey,
					URL:        publicURL,
					OwnerType:  "learning_node_video",
					OwnerID:    row.ID,
					Metadata:   datatypes.JSON(b),
					CreatedAt:  time.Now().UTC(),
					UpdatedAt:  time.Now().UTC(),
				}
				if _, err := deps.Assets.Create(dbctx.Context{Ctx: ctx}, []*types.Asset{a}); err == nil {
					assetID = &aid
				}
			}

			now := time.Now().UTC()
			update := &types.LearningNodeVideo{
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
			_ = deps.Videos.Upsert(dbctx.Context{Ctx: ctx}, update)

			if deps.GenRuns != nil {
				metrics := map[string]any{
					"storage_key":  storageKey,
					"url":          publicURL,
					"mime_type":    mime,
					"byte_len":     len(vid.Bytes),
					"duration_sec": dur,
				}
				_, _ = deps.GenRuns.Create(dbctx.Context{Ctx: ctx}, []*types.LearningDocGenerationRun{
					makeGenRun("node_video_asset", &update.ID, in.OwnerUserID, pathID, row.PathNodeID, "succeeded", nodeVideoAssetPromptVersion, 1, latency, nil, metrics),
				})
			}

			atomic.AddInt32(&rendered, 1)
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return out, err
	}

	out.VideosRendered = int(atomic.LoadInt32(&rendered))
	out.VideosFailed = int(atomic.LoadInt32(&failed))

	return out, nil
}

func markVideoFailed(ctx context.Context, deps NodeVideosRenderDeps, row *types.LearningNodeVideo, errMsg string, latencyMS int) error {
	if row == nil || deps.Videos == nil {
		return nil
	}
	errMsg = strings.TrimSpace(errMsg)
	if len(errMsg) > 900 {
		errMsg = errMsg[:900]
	}
	now := time.Now().UTC()
	update := &types.LearningNodeVideo{
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
	_ = deps.Videos.Upsert(dbctx.Context{Ctx: ctx}, update)

	if deps.GenRuns != nil {
		_, _ = deps.GenRuns.Create(dbctx.Context{Ctx: ctx}, []*types.LearningDocGenerationRun{
			makeGenRun("node_video_asset", &row.ID, row.UserID, row.PathID, row.PathNodeID, "failed", nodeVideoAssetPromptVersion, 1, latencyMS, []string{errMsg}, nil),
		})
	}

	return nil
}
