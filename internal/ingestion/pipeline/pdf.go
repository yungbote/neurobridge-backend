package pipeline

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/yungbote/neurobridge-backend/internal/clients/gcp"
	"github.com/yungbote/neurobridge-backend/internal/ingestion/extractor"
	"github.com/yungbote/neurobridge-backend/internal/clients/localmedia"
	"github.com/yungbote/neurobridge-backend/internal/types"
)

func (s *service) handlePDF(ctx context.Context, mf *types.MaterialFile, pdfPath string) ([]Segment, []AssetRef, []string, map[string]any, error) {
	diag := map[string]any{"pipeline": "pdf"}
	var warnings []string
	var assets []AssetRef
	var segs []Segment

	docAIRes, docAIErr := s.ex.TryDocAI(ctx, "application/pdf", mf.StorageKey)
	if docAIErr != nil {
		warnings = append(warnings, "docai failed: "+docAIErr.Error())
		diag["docai_error"] = docAIErr.Error()
	} else {
		diag["docai_primary_text_len"] = len(docAIRes.PrimaryText)
		segs = append(segs, extractor.TagSegments(docAIRes.Segments, map[string]any{"kind": "docai_page_text"})...)
		segs = append(segs, extractor.TagSegments(docAIRes.Tables, map[string]any{"kind": "table_text"})...)
		segs = append(segs, extractor.TagSegments(docAIRes.Forms, map[string]any{"kind": "form_text"})...)
	}

	if extractor.TextSignalWeak(segs) {
		if s.ex.VisionOutputPrefix == "" || s.ex.MaterialBucketName == "" {
			warnings = append(warnings, "vision OCR fallback skipped (missing VISION_OCR_OUTPUT_PREFIX or MATERIAL_GCS_BUCKET_NAME)")
		} else if s.ex.Vision != nil {
			gcsURI := fmt.Sprintf("gs://%s/%s", s.ex.MaterialBucketName, mf.StorageKey)
			outPrefix := fmt.Sprintf("%s%s/%s/", extractor.EnsureGSPrefix(s.ex.VisionOutputPrefix), mf.MaterialSetID.String(), mf.ID.String())
			vres, err := s.ex.Vision.OCRFileInGCS(ctx, gcsURI, "application/pdf", outPrefix, s.ex.MaxPDFPagesRender)
			if err != nil {
				warnings = append(warnings, "vision OCR failed: "+err.Error())
				diag["vision_error"] = err.Error()
			} else {
				diag["vision_primary_text_len"] = len(vres.PrimaryText)
				for _, sg := range vres.Segments {
					if sg.Metadata == nil {
						sg.Metadata = map[string]any{}
					}
					sg.Metadata["kind"] = "ocr_text"
					sg.Metadata["provider"] = "gcp_vision_async"
					segs = append(segs, sg)
				}
			}
		}
	}

	pageImages, pageAssets, renderWarn, renderDiag := s.renderPDFPagesToGCS(ctx, mf, pdfPath)
	assets = append(assets, pageAssets...)
	warnings = append(warnings, renderWarn...)
	extractor.MergeDiag(diag, renderDiag)

	if s.ex.Caption != nil && len(pageImages) > 0 {
		capN := extractor.MinInt(len(pageImages), s.ex.MaxPDFPagesCaption)
		for i := 0; i < capN; i++ {
			page := i + 1
			imgAsset := pageImages[i]
			noteSegs, warn, err := s.captionAssetToSegments(ctx, "figure_notes", imgAsset, page, nil, nil)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("caption page %d failed: %v", page, err))
				continue
			}
			if warn != "" {
				warnings = append(warnings, warn)
			}
			segs = append(segs, noteSegs...)
		}
	} else {
		warnings = append(warnings, "caption provider unavailable; figure_notes skipped")
	}

	return segs, assets, warnings, diag, nil
}

func (s *service) renderPDFPagesToGCS(ctx context.Context, mf *types.MaterialFile, pdfPath string) ([]AssetRef, []AssetRef, []string, map[string]any) {
	diag := map[string]any{"render": "pdftoppm"}
	var warnings []string

	if s.ex.Media == nil {
		return nil, nil, []string{"media tools unavailable; cannot render pdf pages"}, diag
	}

	tmpDir, err := os.MkdirTemp("", "nb_pdf_pages_*")
	if err != nil {
		return nil, nil, []string{"temp dir error: " + err.Error()}, diag
	}
	defer os.RemoveAll(tmpDir)

	paths, err := s.ex.Media.RenderPDFToImages(ctx, pdfPath, tmpDir, localmedia.PDFRenderOptions{
		DPI:       200,
		Format:    "png",
		FirstPage: 0,
		LastPage:  0,
	})
	if err != nil {
		return nil, nil, []string{"pdf render failed: " + err.Error()}, diag
	}

	if len(paths) > s.ex.MaxPDFPagesRender {
		warnings = append(warnings, fmt.Sprintf("pdf pages truncated: rendered %d capped to %d", len(paths), s.ex.MaxPDFPagesRender))
		paths = paths[:s.ex.MaxPDFPagesRender]
	}

	pageAssets := make([]AssetRef, 0, len(paths))
	for i, pth := range paths {
		pageNum := i + 1
		key := fmt.Sprintf("%s/derived/pages/page_%04d.png", mf.StorageKey, pageNum)
		if err := s.ex.UploadLocalToGCS(ctx, nil, key, pth); err != nil {
			warnings = append(warnings, fmt.Sprintf("upload page %d failed: %v", pageNum, err))
			continue
		}
		pageAssets = append(pageAssets, AssetRef{
			Kind: "pdf_page",
			Key:  key,
			URL:  s.ex.Bucket.GetPublicURL(gcp.BucketCategoryMaterial, key),
			Metadata: map[string]any{
				"page":   pageNum,
				"format": "png",
			},
		})
	}

	diag["pages_rendered"] = len(pageAssets)
	return pageAssets, pageAssets, warnings, diag
}

// keep identical imports usage
var _ = strings.TrimSpace










