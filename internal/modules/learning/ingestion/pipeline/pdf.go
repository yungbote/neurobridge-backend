package pipeline

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/ingestion/extractor"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/gcp"
	"github.com/yungbote/neurobridge-backend/internal/platform/localmedia"
)

func (s *service) handlePDF(ctx context.Context, mf *types.MaterialFile, pdfPath string) ([]Segment, []AssetRef, []string, map[string]any, error) {
	diag := map[string]any{"pipeline": "pdf"}
	var warnings []string
	var assets []AssetRef
	var segs []Segment

	if err := ctx.Err(); err != nil {
		return nil, nil, nil, diag, err
	}

	var docAIRes *gcp.DocAIResult
	var docAIErr error

	// Prefer local PDF bytes when we have a local pdfPath (e.g., PPTX->PDF)
	if strings.TrimSpace(pdfPath) != "" && s.ex.DocAI != nil {
		b, rerr := os.ReadFile(pdfPath)
		if rerr != nil {
			docAIErr = fmt.Errorf("read local pdf for docai: %w", rerr)
		} else if s.ex.DocAIProjectID == "" || s.ex.DocAIProcessorID == "" || s.ex.DocAILocation == "" {
			docAIErr = fmt.Errorf("missing docai env (GCP_PROJECT_ID, DOCUMENTAI_LOCATION, DOCUMENTAI_PROCESSOR_ID)")
		} else {
			docAIRes, docAIErr = s.ex.DocAI.ProcessBytes(ctx, gcp.DocAIProcessBytesRequest{
				ProjectID:        s.ex.DocAIProjectID,
				Location:         s.ex.DocAILocation,
				ProcessorID:      s.ex.DocAIProcessorID,
				ProcessorVersion: s.ex.DocAIProcessorVer,
				MimeType:         "application/pdf",
				Data:             b,
				FieldMask:        nil,
			})
		}
	} else {
		// Original behavior for true PDFs already in GCS
		docAIRes, docAIErr = s.ex.TryDocAI(ctx, "application/pdf", mf.StorageKey)
	}

	if docAIErr != nil {
		warnings = append(warnings, "docai failed: "+docAIErr.Error())
		diag["docai_error"] = docAIErr.Error()
	} else if docAIRes != nil {
		diag["docai_primary_text_len"] = len(docAIRes.PrimaryText)
		segs = append(segs, extractor.TagSegments(docAIRes.Segments, map[string]any{"kind": "docai_page_text"})...)
		segs = append(segs, extractor.TagSegments(docAIRes.Tables, map[string]any{"kind": "table_text"})...)
		segs = append(segs, extractor.TagSegments(docAIRes.Forms, map[string]any{"kind": "form_text"})...)
	}

	// Local fallback: if DocAI fails/unavailable for a local PDF (e.g., PPTX->PDF),
	// attempt `pdftotext` so we still get usable text signals for downstream jobs.
	if len(segs) == 0 && strings.TrimSpace(pdfPath) != "" {
		txt, err := pdfToTextLocal(ctx, pdfPath)
		if err != nil {
			warnings = append(warnings, "pdftotext fallback failed: "+err.Error())
			diag["pdftotext_error"] = err.Error()
		} else if strings.TrimSpace(txt) != "" {
			diag["pdftotext_len"] = len(txt)
			segs = append(segs, Segment{
				Text: txt,
				Metadata: map[string]any{
					"kind":     "pdftotext",
					"provider": "local_pdftotext",
				},
			})
		}
	}

	if extractor.TextSignalWeak(segs) {
		if s.ex.VisionOutputPrefix == "" || s.ex.MaterialBucketName == "" {
			warnings = append(warnings, "vision OCR fallback skipped (missing VISION_OCR_OUTPUT_PREFIX or MATERIAL_GCS_BUCKET_NAME)")
		} else if s.ex.Vision != nil {
			var (
				gcsURI       string
				visionSource string
			)

			// Prefer OCR on the original PDF in GCS when the upload itself is a PDF.
			if strings.EqualFold(strings.TrimSpace(mf.MimeType), "application/pdf") {
				gcsURI = fmt.Sprintf("gs://%s/%s", s.ex.MaterialBucketName, mf.StorageKey)
				visionSource = "material_pdf"
			} else if strings.TrimSpace(pdfPath) != "" {
				// For office->pdf conversions, upload the derived PDF and OCR that.
				derivedKey := strings.TrimRight(mf.StorageKey, "/") + "/derived/source.pdf"
				if err := s.ex.UploadLocalToGCS(dbctx.Context{Ctx: ctx}, derivedKey, pdfPath); err != nil {
					warnings = append(warnings, "vision OCR fallback skipped (upload derived pdf failed): "+err.Error())
				} else {
					gcsURI = fmt.Sprintf("gs://%s/%s", s.ex.MaterialBucketName, derivedKey)
					visionSource = "derived_pdf"
					diag["vision_input_key"] = derivedKey
				}
			} else {
				warnings = append(warnings, "vision OCR fallback skipped (no PDF source available)")
			}

			if strings.TrimSpace(gcsURI) != "" {
				diag["vision_source"] = visionSource
				diag["vision_input_gcs_uri"] = gcsURI
				outPrefix := fmt.Sprintf(
					"%s%s/%s/",
					extractor.EnsureGSPrefix(s.ex.VisionOutputPrefix),
					mf.MaterialSetID.String(),
					mf.ID.String(),
				)
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
						if strings.TrimSpace(visionSource) != "" {
							sg.Metadata["ocr_source"] = visionSource
						}
						segs = append(segs, sg)
					}
				}
			}
		}
	}

	pageImages, pageAssets, renderWarn, renderDiag := s.renderPDFPagesToGCS(ctx, mf, pdfPath)
	assets = append(assets, pageAssets...)
	warnings = append(warnings, renderWarn...)
	extractor.MergeDiag(diag, renderDiag)
	if err := ctx.Err(); err != nil {
		return segs, assets, warnings, diag, err
	}

	if s.ex.Caption == nil {
		warnings = append(warnings, "caption provider unavailable; figure_notes skipped")
	} else if len(pageImages) == 0 {
		warnings = append(warnings, "no page images rendered; figure_notes skipped")
	} else {
		capN := extractor.MinInt(len(pageImages), s.ex.MaxPDFPagesCaption)
		for i := 0; i < capN; i++ {
			if err := ctx.Err(); err != nil {
				return segs, assets, warnings, diag, err
			}
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
	}

	return segs, assets, warnings, diag, nil
}

func pdfToTextLocal(ctx context.Context, pdfPath string) (string, error) {
	ctx = extractor.DefaultCtx(ctx)
	if strings.TrimSpace(pdfPath) == "" {
		return "", fmt.Errorf("pdfPath required")
	}
	if _, err := exec.LookPath("pdftotext"); err != nil {
		return "", fmt.Errorf("pdftotext not found in PATH: %w", err)
	}

	callCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	tmpDir, err := os.MkdirTemp("", "nb_pdftotext_*")
	if err != nil {
		return "", fmt.Errorf("temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	outPath := filepath.Join(tmpDir, "out.txt")

	cmd := exec.CommandContext(callCtx, "pdftotext",
		"-enc", "UTF-8",
		"-q",
		pdfPath,
		outPath,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		s := strings.TrimSpace(stderr.String())
		if s != "" {
			return "", fmt.Errorf("pdftotext: %w; stderr=%s", err, s)
		}
		return "", fmt.Errorf("pdftotext: %w", err)
	}

	b, err := os.ReadFile(outPath)
	if err != nil {
		return "", fmt.Errorf("read pdftotext output: %w", err)
	}
	txt := strings.TrimSpace(string(b))
	if txt == "" {
		return "", fmt.Errorf("pdftotext produced empty output")
	}
	return txt, nil
}

func (s *service) renderPDFPagesToGCS(ctx context.Context, mf *types.MaterialFile, pdfPath string) ([]AssetRef, []AssetRef, []string, map[string]any) {
	diag := map[string]any{"render": "pdftoppm"}
	var warnings []string

	if err := ctx.Err(); err != nil {
		warnings = append(warnings, "render canceled: "+err.Error())
		return nil, nil, warnings, diag
	}

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
		if err := ctx.Err(); err != nil {
			warnings = append(warnings, "upload pages canceled: "+err.Error())
			break
		}
		pageNum := i + 1
		key := fmt.Sprintf("%s/derived/pages/page_%04d.png", mf.StorageKey, pageNum)
		if err := s.ex.UploadLocalToGCS(dbctx.Context{Ctx: ctx}, key, pth); err != nil {
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
