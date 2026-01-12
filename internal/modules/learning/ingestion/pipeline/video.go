package pipeline

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/ingestion/extractor"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/gcp"
	"github.com/yungbote/neurobridge-backend/internal/platform/localmedia"
)

func (s *service) handleVideo(ctx context.Context, mf *types.MaterialFile, videoPath string) ([]Segment, []AssetRef, []string, map[string]any, error) {
	diag := map[string]any{"pipeline": "video"}
	var warnings []string
	var assets []AssetRef
	var segs []Segment

	if err := ctx.Err(); err != nil {
		return nil, nil, nil, diag, err
	}

	if s.ex.VideoAI != nil && s.ex.MaterialBucketName != "" {
		gcsURI := fmt.Sprintf("gs://%s/%s", s.ex.MaterialBucketName, mf.StorageKey)
		vres, err := s.ex.VideoAI.AnnotateVideoGCS(ctx, gcsURI, gcp.VideoAIConfig{
			LanguageCode:               "en-US",
			Model:                      "video",
			EnableAutomaticPunctuation: true,
			EnableSpeakerDiarization:   true,
			EnableSpeechTranscription:  true,
			EnableTextDetection:        true,
			EnableShotChangeDetection:  true,
		})
		if err != nil {
			warnings = append(warnings, "video intelligence failed: "+err.Error())
			diag["videoai_error"] = err.Error()
		} else {
			diag["videoai_primary_text_len"] = len(vres.PrimaryText)
			segs = append(segs, vres.TranscriptSegments...)
			segs = append(segs, vres.TextSegments...)
		}
	} else {
		warnings = append(warnings, "video intelligence unavailable or missing MATERIAL_GCS_BUCKET_NAME; skipping")
	}

	if s.ex.Media == nil {
		return segs, assets, append(warnings, "media tools missing: cannot extract audio/frames"), diag, nil
	}

	tmpDir, err := os.MkdirTemp("", "nb_video_*")
	if err != nil {
		return segs, assets, append(warnings, "temp dir error: "+err.Error()), diag, nil
	}
	defer os.RemoveAll(tmpDir)

	if err := ctx.Err(); err != nil {
		return segs, assets, warnings, diag, err
	}

	audioPath := filepath.Join(tmpDir, "audio.wav")
	_, err = s.ex.Media.ExtractAudioFromVideo(ctx, videoPath, audioPath, localmedia.AudioExtractOptions{
		SampleRateHz: 16000,
		Channels:     1,
		Format:       "wav",
	})
	if err != nil {
		warnings = append(warnings, "extract audio failed: "+err.Error())
	} else {
		aSegs, aAssets, aWarn, aDiag, _ := s.handleAudio(ctx, mf, audioPath)
		segs = append(segs, aSegs...)
		assets = append(assets, aAssets...)
		warnings = append(warnings, aWarn...)
		extractor.MergeDiag(diag, aDiag)
	}

	framesDir := filepath.Join(tmpDir, "frames")
	frames, err := s.ex.Media.ExtractKeyframes(ctx, videoPath, framesDir, localmedia.KeyframeOptions{
		IntervalSeconds: s.ex.VideoFrameIntervalSec,
		SceneThreshold:  s.ex.VideoSceneThreshold,
		Width:           1280,
		MaxFrames:       s.ex.MaxFramesVideo,
		Format:          "jpg",
		JPEGQuality:     3,
	})
	if err != nil {
		warnings = append(warnings, "extract keyframes failed: "+err.Error())
		return segs, assets, warnings, diag, nil
	}

	if err := ctx.Err(); err != nil {
		return segs, assets, warnings, diag, err
	}

	if len(frames) > s.ex.MaxFramesVideo {
		warnings = append(warnings, fmt.Sprintf("frames truncated: %d -> %d", len(frames), s.ex.MaxFramesVideo))
		frames = frames[:s.ex.MaxFramesVideo]
	}

	frameAssets := make([]AssetRef, 0, len(frames))
	for i, fp := range frames {
		if err := ctx.Err(); err != nil {
			return segs, assets, warnings, diag, err
		}
		frameIdx := i + 1
		key := fmt.Sprintf("%s/derived/frames/frame_%06d.jpg", mf.StorageKey, frameIdx)
		if err := s.ex.UploadLocalToGCS(dbctx.Context{Ctx: ctx}, key, fp); err != nil {
			warnings = append(warnings, fmt.Sprintf("upload frame %d failed: %v", frameIdx, err))
			continue
		}
		frameAssets = append(frameAssets, AssetRef{
			Kind: "frame",
			Key:  key,
			URL:  s.ex.Bucket.GetPublicURL(gcp.BucketCategoryMaterial, key),
			Metadata: map[string]any{
				"frame_index": frameIdx,
			},
		})
	}
	assets = append(assets, frameAssets...)

	// Preserve original behavior (possible mismatch if uploads failed)
	if s.ex.Vision != nil {
		for i, a := range frameAssets {
			if err := ctx.Err(); err != nil {
				return segs, assets, warnings, diag, err
			}
			localPath := frames[i]
			b, readErr := os.ReadFile(localPath)
			if readErr != nil {
				continue
			}

			ocr, err := s.ex.Vision.OCRImageBytes(ctx, b, "image/jpeg")
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("frame ocr failed (%s): %v", a.Key, err))
			} else if strings.TrimSpace(ocr.PrimaryText) != "" {
				segs = append(segs, Segment{
					Text: ocr.PrimaryText,
					Metadata: map[string]any{
						"kind":        "frame_ocr",
						"asset_key":   a.Key,
						"frame_index": a.Metadata["frame_index"],
						"provider":    "gcp_vision",
					},
				})
			}

			if s.ex.Caption != nil {
				noteSegs, warn, err := s.captionAssetToSegments(ctx, "frame_notes", a, 0, nil, nil)
				if err != nil {
					warnings = append(warnings, fmt.Sprintf("frame caption failed (%s): %v", a.Key, err))
				} else {
					if warn != "" {
						warnings = append(warnings, warn)
					}
					segs = append(segs, noteSegs...)
				}
			}

			if i+1 >= s.ex.MaxFramesCaption {
				warnings = append(warnings, fmt.Sprintf("frame caption capped at %d frames", s.ex.MaxFramesCaption))
				break
			}
		}
	} else {
		warnings = append(warnings, "vision provider unavailable; frame OCR skipped")
	}

	return segs, assets, warnings, diag, nil
}
