package steps

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
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

const nodeVideoAssetPromptVersion = "video_asset_v2@1"

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

	maxVideos := envIntAllowZero("NODE_VIDEOS_RENDER_LIMIT", -1)
	if maxVideos == 0 {
		deps.Log.Warn("NODE_VIDEOS_RENDER_LIMIT=0; skipping node_videos_render")
		return out, nil
	}
	if maxVideos > 0 && len(work) > maxVideos {
		work = work[:maxVideos]
	}

	maxConc := envInt("NODE_VIDEOS_RENDER_CONCURRENCY", 1)
	if maxConc < 1 {
		maxConc = 1
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

			var plan content.VideoPlanItemV2
			if len(row.PlanJSON) == 0 || strings.TrimSpace(string(row.PlanJSON)) == "" || string(row.PlanJSON) == "null" {
				return markVideoFailed(gctx, deps, row, "missing_plan_json", 0)
			}
			if err := json.Unmarshal(row.PlanJSON, &plan); err != nil {
				return markVideoFailed(gctx, deps, row, "invalid_plan_json", 0)
			}

			clips := plan.Storyboard.Clips
			if len(clips) == 0 {
				// Back-compat for legacy plans: single prompt + duration.
				prompt := strings.TrimSpace(plan.Prompt)
				if prompt == "" {
					return markVideoFailed(gctx, deps, row, "empty_prompt", 0)
				}
				dur := plan.DurationSec
				if dur <= 0 {
					dur = 8
				}
				clips = []content.VideoClipV1{{ClipIndex: 1, DurationSec: dur, Prompt: prompt}}
			}

			maxClips := envIntAllowZero("NODE_VIDEO_MAX_CLIPS_PER_VIDEO", 4)
			if maxClips <= 0 {
				maxClips = 4
			}
			if len(clips) > maxClips {
				return markVideoFailed(gctx, deps, row, fmt.Sprintf("too_many_clips: %d > %d", len(clips), maxClips), int(time.Since(start).Milliseconds()))
			}

			// Guardrails: keep videos clean and learner-safe without forcing a specific visual style.
			constraints := "Hard constraints: short educational video; no watermarks; no logos; no brand names; avoid identifiable people/faces; keep it faithful to the described concept."

			finalBytes := []byte(nil)
			finalMime := "video/mp4"
			revisedPrompts := make([]string, 0, len(clips))

			tmpDir, err := os.MkdirTemp("", "nb-video-stitch-*")
			if err != nil {
				_ = markVideoFailed(gctx, deps, row, "tmpdir_failed: "+err.Error(), int(time.Since(start).Milliseconds()))
				atomic.AddInt32(&failed, 1)
				return nil
			}
			defer func() { _ = os.RemoveAll(tmpDir) }()

			clipPaths := make([]string, 0, len(clips))
			plannedClipDurations := make([]int, 0, len(clips))

			for idx := range clips {
				c := clips[idx]
				prompt := strings.TrimSpace(c.Prompt)
				if prompt == "" {
					_ = markVideoFailed(gctx, deps, row, "empty_clip_prompt", int(time.Since(start).Milliseconds()))
					atomic.AddInt32(&failed, 1)
					return nil
				}
				dur := c.DurationSec
				if dur <= 0 {
					dur = 8
				}
				plannedClipDurations = append(plannedClipDurations, dur)

				prompt = prompt + "\n\n" + constraints

				vid, err := deps.AI.GenerateVideo(gctx, prompt, openai.VideoGenerationOptions{DurationSeconds: dur})
				if err != nil {
					_ = markVideoFailed(gctx, deps, row, "video_generate_failed: "+err.Error(), int(time.Since(start).Milliseconds()))
					atomic.AddInt32(&failed, 1)
					return nil
				}
				if len(vid.Bytes) == 0 {
					_ = markVideoFailed(gctx, deps, row, "video_generate_empty", int(time.Since(start).Milliseconds()))
					atomic.AddInt32(&failed, 1)
					return nil
				}
				if strings.TrimSpace(vid.RevisedPrompt) != "" {
					revisedPrompts = append(revisedPrompts, strings.TrimSpace(vid.RevisedPrompt))
				}

				mime := strings.TrimSpace(vid.MimeType)
				ext := "mp4"
				switch {
				case strings.Contains(strings.ToLower(mime), "webm"):
					ext = "webm"
				case strings.Contains(strings.ToLower(mime), "mp4"):
					ext = "mp4"
				}
				clipPath := filepath.Join(tmpDir, fmt.Sprintf("clip_%02d.%s", idx+1, ext))
				if werr := os.WriteFile(clipPath, vid.Bytes, 0o600); werr != nil {
					_ = markVideoFailed(gctx, deps, row, "tmp_write_failed: "+werr.Error(), int(time.Since(start).Milliseconds()))
					atomic.AddInt32(&failed, 1)
					return nil
				}
				clipPaths = append(clipPaths, clipPath)
			}

			stitchedPath := filepath.Join(tmpDir, "stitched.mp4")
			if err := stitchVideoFiles(gctx, clipPaths, plannedClipDurations, stitchedPath); err != nil {
				_ = markVideoFailed(gctx, deps, row, "stitch_failed: "+err.Error(), int(time.Since(start).Milliseconds()))
				atomic.AddInt32(&failed, 1)
				return nil
			}
			b, rerr := os.ReadFile(stitchedPath)
			if rerr != nil || len(b) == 0 {
				msg := "stitched_read_failed"
				if rerr != nil {
					msg = msg + ": " + rerr.Error()
				}
				_ = markVideoFailed(gctx, deps, row, msg, int(time.Since(start).Milliseconds()))
				atomic.AddInt32(&failed, 1)
				return nil
			}
			finalBytes = b

			latency := int(time.Since(start).Milliseconds())

			mime := strings.TrimSpace(finalMime)
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
			if err := deps.Bucket.UploadFile(dbctx.Context{Ctx: ctx}, gcp.BucketCategoryMaterial, storageKey, bytes.NewReader(finalBytes)); err != nil {
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

			totalDuration := plan.DurationSec
			if plan.Storyboard.TotalDurationSec > 0 {
				totalDuration = plan.Storyboard.TotalDurationSec
			}
			if totalDuration <= 0 {
				totalDuration = 8
			}
			clipDurations := plannedClipDurations

			var assetID *uuid.UUID
			if deps.Assets != nil {
				meta := map[string]any{
					"asset_kind":         "generated_video",
					"semantic_type":      strings.TrimSpace(plan.SemanticType),
					"caption":            strings.TrimSpace(plan.Caption),
					"alt_text":           strings.TrimSpace(plan.AltText),
					"placement_hint":     strings.TrimSpace(plan.PlacementHint),
					"citations":          content.NormalizeConceptKeys(plan.Citations),
					"prompt_hash":        strings.TrimSpace(row.PromptHash),
					"sources_hash":       strings.TrimSpace(row.SourcesHash),
					"duration_sec":       totalDuration,
					"clips_count":        len(clips),
					"clip_durations_sec": clipDurations,
					"stitched":           len(clips) > 1,
					"revised_prompts":    revisedPrompts,
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
					"byte_len":     len(finalBytes),
					"duration_sec": totalDuration,
					"clips_count":  len(clips),
					"stitched":     len(clips) > 1,
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

func stitchVideoFiles(ctx context.Context, inputs []string, plannedDurationsSec []int, outPath string) error {
	return stitchVideoFilesSeamless(ctx, inputs, plannedDurationsSec, outPath)
}

func runFFmpeg(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	msg := strings.TrimSpace(string(out))
	if msg == "" {
		msg = err.Error()
	}
	if len(msg) > 900 {
		msg = msg[:900]
	}
	return fmt.Errorf("ffmpeg: %s", msg)
}

func stitchVideoFilesSeamless(ctx context.Context, inputs []string, plannedDurationsSec []int, outPath string) error {
	if len(inputs) == 0 {
		return fmt.Errorf("no inputs")
	}
	dir := filepath.Dir(outPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	targetW, targetH := parseVideoSize(strings.TrimSpace(os.Getenv("OPENAI_VIDEO_SIZE")))
	if targetW <= 0 || targetH <= 0 {
		targetW, targetH = 1280, 720
	}
	// yuv420p requires even dimensions.
	if targetW%2 == 1 {
		targetW--
	}
	if targetH%2 == 1 {
		targetH--
	}
	if targetW <= 0 || targetH <= 0 {
		targetW, targetH = 1280, 720
	}

	fps := envIntAllowZero("NODE_VIDEO_STITCH_FPS", 30)
	if fps <= 0 {
		fps = 30
	}
	crossfadeMS := envIntAllowZero("NODE_VIDEO_STITCH_CROSSFADE_MS", 250)
	if crossfadeMS < 0 {
		crossfadeMS = 0
	}
	holdEndMS := envIntAllowZero("NODE_VIDEO_STITCH_HOLD_END_MS", 300)
	if holdEndMS < 0 {
		holdEndMS = 0
	}
	fadeOutMS := envIntAllowZero("NODE_VIDEO_STITCH_FADE_OUT_MS", 250)
	if fadeOutMS < 0 {
		fadeOutMS = 0
	}
	crf := envIntAllowZero("NODE_VIDEO_STITCH_CRF", 20)
	if crf <= 0 {
		crf = 20
	}
	preset := strings.TrimSpace(os.Getenv("NODE_VIDEO_STITCH_PRESET"))
	if preset == "" {
		preset = "veryfast"
	}

	durations := make([]float64, 0, len(inputs))
	for i, p := range inputs {
		p = strings.TrimSpace(p)
		if p == "" {
			durations = append(durations, 0)
			continue
		}
		if d, err := probeVideoDurationSec(ctx, p); err == nil && d > 0 {
			durations = append(durations, d)
			continue
		}
		if i < len(plannedDurationsSec) && plannedDurationsSec[i] > 0 {
			durations = append(durations, float64(plannedDurationsSec[i]))
			continue
		}
		durations = append(durations, 8)
	}

	xfadeSec := float64(crossfadeMS) / 1000.0
	if len(inputs) <= 1 {
		xfadeSec = 0
	}
	if xfadeSec < 0.05 {
		xfadeSec = 0
	}
	minDur := 0.0
	for _, d := range durations {
		if d <= 0 {
			continue
		}
		if minDur == 0 || d < minDur {
			minDur = d
		}
	}
	if minDur > 0 && xfadeSec > minDur/2 {
		xfadeSec = minDur / 2
	}

	holdSec := float64(holdEndMS) / 1000.0
	if holdSec < 0.05 {
		holdSec = 0
	}
	fadeSec := float64(fadeOutMS) / 1000.0
	if fadeSec < 0.05 {
		fadeSec = 0
	}
	// Prefer fading inside the held ending frame to avoid “cutting off” content.
	if holdSec > 0 && fadeSec > holdSec {
		fadeSec = holdSec
	}

	// Estimated stitched duration (seconds).
	totalSec := 0.0
	for _, d := range durations {
		if d > 0 {
			totalSec += d
		}
	}
	if xfadeSec > 0 && len(durations) > 1 {
		totalSec -= float64(len(durations)-1) * xfadeSec
	}
	if totalSec <= 0 {
		totalSec = 8
	}

	fadeStart := totalSec + holdSec - fadeSec
	if fadeStart < 0 {
		fadeStart = 0
	}

	var filter strings.Builder
	// Normalize every input to a common shape/timebase before stitching.
	for i := range inputs {
		filter.WriteString(fmt.Sprintf(
			"[%d:v:0]scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2,fps=%d,format=yuv420p,setsar=1,setpts=PTS-STARTPTS[v%d];",
			i, targetW, targetH, targetW, targetH, fps, i,
		))
	}

	lastLabel := "v0"
	if len(inputs) == 1 {
		// No stitching, just polish: stable ending frame + fade out.
		filter.WriteString("[v0]")
		if holdSec > 0 {
			filter.WriteString(fmt.Sprintf("tpad=stop_mode=clone:stop_duration=%.3f", holdSec))
			if fadeSec > 0 {
				filter.WriteString(fmt.Sprintf(",fade=t=out:st=%.3f:d=%.3f", fadeStart, fadeSec))
			}
		} else if fadeSec > 0 {
			// Fade the tail of the clip if we aren't holding.
			filter.WriteString(fmt.Sprintf("fade=t=out:st=%.3f:d=%.3f", fadeStart, fadeSec))
		}
		filter.WriteString("[outv]")
	} else if xfadeSec > 0 {
		running := durations[0]
		for i := 1; i < len(inputs); i++ {
			offset := running - xfadeSec
			if offset < 0 {
				offset = 0
			}
			outLabel := fmt.Sprintf("x%d", i)
			filter.WriteString(fmt.Sprintf("[%s][v%d]xfade=transition=fade:duration=%.3f:offset=%.3f[%s];", lastLabel, i, xfadeSec, offset, outLabel))
			running = running + durations[i] - xfadeSec
			lastLabel = outLabel
		}

		filter.WriteString(fmt.Sprintf("[%s]", lastLabel))
		if holdSec > 0 {
			filter.WriteString(fmt.Sprintf("tpad=stop_mode=clone:stop_duration=%.3f", holdSec))
			if fadeSec > 0 {
				filter.WriteString(fmt.Sprintf(",fade=t=out:st=%.3f:d=%.3f", fadeStart, fadeSec))
			}
		} else if fadeSec > 0 {
			filter.WriteString(fmt.Sprintf("fade=t=out:st=%.3f:d=%.3f", fadeStart, fadeSec))
		}
		filter.WriteString("[outv]")
	} else {
		// Simple concat with shared formatting (no transition).
		for i := 0; i < len(inputs); i++ {
			filter.WriteString(fmt.Sprintf("[v%d]", i))
		}
		filter.WriteString(fmt.Sprintf("concat=n=%d:v=1:a=0[vcat];", len(inputs)))
		filter.WriteString("[vcat]")
		if holdSec > 0 {
			filter.WriteString(fmt.Sprintf("tpad=stop_mode=clone:stop_duration=%.3f", holdSec))
			if fadeSec > 0 {
				filter.WriteString(fmt.Sprintf(",fade=t=out:st=%.3f:d=%.3f", fadeStart, fadeSec))
			}
		} else if fadeSec > 0 {
			filter.WriteString(fmt.Sprintf("fade=t=out:st=%.3f:d=%.3f", fadeStart, fadeSec))
		}
		filter.WriteString("[outv]")
	}

	args := []string{"-hide_banner", "-loglevel", "error", "-y"}
	for _, p := range inputs {
		args = append(args, "-i", p)
	}
	args = append(args,
		"-filter_complex", filter.String(),
		"-map", "[outv]",
		"-an",
		"-c:v", "libx264",
		"-pix_fmt", "yuv420p",
		"-preset", preset,
		"-crf", strconv.Itoa(crf),
		"-movflags", "+faststart",
		outPath,
	)
	return runFFmpeg(ctx, args...)
}

func probeVideoDurationSec(ctx context.Context, path string) (float64, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return 0, fmt.Errorf("empty path")
	}
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		path,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		if len(msg) > 900 {
			msg = msg[:900]
		}
		return 0, fmt.Errorf("ffprobe: %s", msg)
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return 0, fmt.Errorf("ffprobe: empty duration")
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v <= 0 {
		return 0, fmt.Errorf("ffprobe: invalid duration: %q", s)
	}
	return v, nil
}

func parseVideoSize(s string) (w int, h int) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return 0, 0
	}
	parts := strings.Split(s, "x")
	if len(parts) != 2 {
		return 0, 0
	}
	ww, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	hh, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err1 != nil || err2 != nil || ww <= 0 || hh <= 0 {
		return 0, 0
	}
	return ww, hh
}
