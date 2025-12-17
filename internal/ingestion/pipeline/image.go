package pipeline

import (
	"context"
	"os"
	"strings"

	"github.com/yungbote/neurobridge-backend/internal/types"
)

func (s *service) handleImage(ctx context.Context, mf *types.MaterialFile, imgBytes []byte, imgPath string) ([]Segment, []AssetRef, []string, map[string]any, error) {
	diag := map[string]any{"pipeline": "image"}
	var warnings []string
	var assets []AssetRef
	var segs []Segment

	if len(imgBytes) == 0 && strings.TrimSpace(imgPath) != "" {
		if b, err := os.ReadFile(imgPath); err == nil {
			imgBytes = b
		}
	}

	if s.ex.Vision != nil && len(imgBytes) > 0 {
		ocr, err := s.ex.Vision.OCRImageBytes(ctx, imgBytes, mf.MimeType)
		if err != nil {
			warnings = append(warnings, "vision image ocr failed: "+err.Error())
			diag["vision_error"] = err.Error()
		} else {
			diag["vision_primary_text_len"] = len(ocr.PrimaryText)
			if strings.TrimSpace(ocr.PrimaryText) != "" {
				segs = append(segs, Segment{
					Text: ocr.PrimaryText,
					Metadata: map[string]any{
						"kind":     "ocr_text",
						"provider": "gcp_vision",
					},
				})
			}
		}
	} else if s.ex.Vision == nil {
		warnings = append(warnings, "vision provider unavailable; image OCR skipped")
	}

	// Preserve exact old behavior: caption via bytes directly in-image handler
	if s.ex.Caption != nil && len(imgBytes) > 0 {
		noteSegs, warn, err := s.captionBytesToSegments(ctx, "image_notes", mf.StorageKey, mf.MimeType, imgBytes, 0)
		if err != nil {
			warnings = append(warnings, "caption image failed: "+err.Error())
		} else {
			if warn != "" {
				warnings = append(warnings, warn)
			}
			segs = append(segs, noteSegs...)
		}
	} else if s.ex.Caption == nil {
		warnings = append(warnings, "caption provider unavailable; image_notes skipped")
	} else if len(imgBytes) == 0 {
		warnings = append(warnings, "caption skipped: no image bytes available")
	}

	return segs, assets, warnings, diag, nil
}










