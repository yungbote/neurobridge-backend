package pipeline

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/ingestion/extractor"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/ingestion/outline"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/gcp"
	"github.com/yungbote/neurobridge-backend/internal/platform/localmedia"
	"golang.org/x/sync/errgroup"
)

func countSegmentPages(segs []Segment) int {
	if len(segs) == 0 {
		return 0
	}
	seen := map[int]bool{}
	for _, s := range segs {
		if s.Page == nil {
			continue
		}
		p := *s.Page
		if p <= 0 {
			continue
		}
		seen[p] = true
	}
	return len(seen)
}

func mergeDocAIResults(results []*gcp.DocAIResult) *gcp.DocAIResult {
	if len(results) == 0 {
		return nil
	}
	out := &gcp.DocAIResult{Provider: "gcp_documentai"}
	var primary strings.Builder
	for _, r := range results {
		if r == nil {
			continue
		}
		if out.Processor == "" && strings.TrimSpace(r.Processor) != "" {
			out.Processor = r.Processor
		}
		if out.MimeType == "" && strings.TrimSpace(r.MimeType) != "" {
			out.MimeType = r.MimeType
		}
		if strings.TrimSpace(r.PrimaryText) != "" {
			if primary.Len() > 0 {
				primary.WriteString("\n")
			}
			primary.WriteString(r.PrimaryText)
		}
		out.Segments = append(out.Segments, r.Segments...)
		out.Tables = append(out.Tables, r.Tables...)
		out.Forms = append(out.Forms, r.Forms...)
	}
	out.PrimaryText = strings.TrimSpace(primary.String())
	return out
}

func (s *service) handlePDF(ctx context.Context, mf *types.MaterialFile, pdfPath string) ([]Segment, []AssetRef, []string, map[string]any, error) {
	diag := map[string]any{"pipeline": "pdf"}
	var warnings []string
	var assets []AssetRef
	var segs []Segment
	var pdfPageCount int

	if err := ctx.Err(); err != nil {
		return nil, nil, nil, diag, err
	}

	if s.ex.Media != nil && strings.TrimSpace(pdfPath) != "" {
		if n, err := s.ex.Media.CountPDFPages(ctx, pdfPath); err == nil && n > 0 {
			pdfPageCount = n
			diag["pdf_page_count"] = pdfPageCount
		} else if err != nil {
			diag["pdf_page_count_error"] = err.Error()
		}
	}

	var docAIRes *gcp.DocAIResult
	var docAIErr error
	docAIProcessor := gcp.ProcessorName(s.ex.DocAIProjectID, s.ex.DocAILocation, s.ex.DocAIProcessorID, s.ex.DocAIProcessorVer)
	batchAttempted := false
	var docAIBatchErr error
	if pdfPageCount > 200 && s.ex.DocAI != nil && s.ex.Bucket != nil && s.ex.MaterialBucketName != "" {
		batchAttempted = true
		inputKey := fmt.Sprintf("extraction/docai/input/%s/%s/source.pdf", mf.MaterialSetID.String(), mf.ID.String())
		outputPrefix := fmt.Sprintf("extraction/docai/output/%s/%s/", mf.MaterialSetID.String(), mf.ID.String())
		diag["docai_batch_input_key"] = inputKey
		diag["docai_batch_output_prefix"] = outputPrefix

		if strings.EqualFold(strings.TrimSpace(mf.MimeType), "application/pdf") && strings.TrimSpace(mf.StorageKey) != "" {
			if mf.StorageKey != inputKey {
				if err := s.ex.Bucket.CopyObject(ctx, gcp.BucketCategoryMaterial, mf.StorageKey, inputKey); err != nil {
					docAIErr = fmt.Errorf("docai batch copy input: %w", err)
					docAIBatchErr = docAIErr
				}
			}
		} else if strings.TrimSpace(pdfPath) != "" {
			if err := s.ex.UploadLocalToGCS(dbctx.Context{Ctx: ctx}, inputKey, pdfPath); err != nil {
				docAIErr = fmt.Errorf("docai batch upload input: %w", err)
				docAIBatchErr = docAIErr
			}
		} else {
			docAIErr = fmt.Errorf("docai batch missing input pdf")
			docAIBatchErr = docAIErr
		}

		if docAIErr == nil {
			_ = s.ex.Bucket.DeletePrefix(ctx, gcp.BucketCategoryMaterial, outputPrefix)
			batchRes, err := s.ex.DocAI.BatchProcessGCS(ctx, gcp.DocAIBatchRequest{
				ProjectID:        s.ex.DocAIProjectID,
				Location:         s.ex.DocAILocation,
				ProcessorID:      s.ex.DocAIProcessorID,
				ProcessorVersion: s.ex.DocAIProcessorVer,
				MimeType:         "application/pdf",
				InputGCSURI:      fmt.Sprintf("gs://%s/%s", s.ex.MaterialBucketName, inputKey),
				OutputGCSURI:     fmt.Sprintf("gs://%s/%s", s.ex.MaterialBucketName, outputPrefix),
			})
			if err != nil {
				docAIErr = fmt.Errorf("docai batch process: %w", err)
				docAIBatchErr = docAIErr
			} else {
				diag["docai_batch_docs"] = batchRes.Documents
				results := make([]*gcp.DocAIResult, 0, batchRes.Documents)
				var batchWarns []string
				for _, key := range batchRes.OutputObjects {
					if !strings.HasSuffix(strings.ToLower(key), ".json") {
						continue
					}
					rc, rerr := s.ex.Bucket.DownloadFile(ctx, gcp.BucketCategoryMaterial, key)
					if rerr != nil {
						batchWarns = append(batchWarns, fmt.Sprintf("docai batch download failed: %s: %v", key, rerr))
						continue
					}
					b, rerr := io.ReadAll(rc)
					_ = rc.Close()
					if rerr != nil {
						batchWarns = append(batchWarns, fmt.Sprintf("docai batch read failed: %s: %v", key, rerr))
						continue
					}
					res, rerr := gcp.ParseDocAIJSON(b, docAIProcessor, "application/pdf")
					if rerr != nil {
						batchWarns = append(batchWarns, fmt.Sprintf("docai batch parse failed: %s: %v", key, rerr))
						continue
					}
					results = append(results, res)
				}
				if len(batchWarns) > 0 {
					warnings = append(warnings, batchWarns...)
				}
				docAIRes = mergeDocAIResults(results)
				if docAIRes == nil {
					docAIErr = fmt.Errorf("docai batch produced no documents")
					docAIBatchErr = docAIErr
				}
			}
		}
	}
	if docAIBatchErr != nil {
		diag["docai_batch_error"] = docAIBatchErr.Error()
	}

	// Prefer local PDF bytes when we have a local pdfPath (e.g., PPTX->PDF)
	if docAIRes == nil && strings.TrimSpace(pdfPath) != "" && s.ex.DocAI != nil && (docAIErr != nil || !batchAttempted) {
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
	} else if docAIRes == nil && (docAIErr != nil || !batchAttempted) {
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

	textLen := extractor.TextSignalLen(segs)
	textWeak := textLen < 500
	if !textWeak && mf != nil && mf.SizeBytes > 0 {
		minChars := mf.SizeBytes / 200
		if minChars < 500 {
			minChars = 500
		}
		if int64(textLen) < minChars {
			textWeak = true
			diag["text_signal_len"] = textLen
			diag["text_signal_min_chars"] = minChars
			diag["text_signal_size_bytes"] = mf.SizeBytes
		}
	}
	docAIPages := countSegmentPages(segs)
	if docAIPages > 0 {
		diag["docai_pages_found"] = docAIPages
	}
	if !textWeak && pdfPageCount > 0 {
		minCharsPerPage := 200
		minCharsByPages := pdfPageCount * minCharsPerPage
		if textLen < minCharsByPages {
			textWeak = true
			diag["text_signal_len"] = textLen
			diag["text_signal_min_chars_per_page"] = minCharsPerPage
			diag["text_signal_min_chars_pages"] = minCharsByPages
		}
		if docAIPages == 0 && pdfPageCount >= 20 {
			textWeak = true
			diag["text_signal_reason"] = "missing_docai_pages"
		}
	}
	if textWeak {
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
		capN := len(pageImages)
		if s.ex.MaxPDFPagesCaption > 0 {
			capN = extractor.MinInt(len(pageImages), s.ex.MaxPDFPagesCaption)
		}
		conceptN := 0
		if diagramConceptsEnabled() {
			maxConceptCeiling := envIntAllowZero("DIAGRAM_CONCEPTS_MAX_PAGES", 0)
			if maxConceptCeiling <= 0 {
				maxConceptCeiling = s.ex.MaxPDFPagesCaption
			}
			adaptiveMax := clampIntCeiling(int(math.Round(float64(len(pageImages))*0.15)), 2, maxConceptCeiling)
			conceptN = extractor.MinInt(len(pageImages), adaptiveMax)
		}
		var (
			capMu          sync.Mutex
			segsByPage     = make([][]Segment, capN)
			conceptsByPage = make([][]Segment, conceptN)
			capWarnings    []string
		)

		g, gctx := errgroup.WithContext(ctx)
		g.SetLimit(10)
		maxPages := capN
		if conceptN > maxPages {
			maxPages = conceptN
		}
		for i := 0; i < maxPages; i++ {
			i := i
			g.Go(func() error {
				if err := gctx.Err(); err != nil {
					return err
				}
				if i >= len(pageImages) {
					return nil
				}
				imgAsset := pageImages[i]
				page := i + 1
				if imgAsset.Metadata != nil {
					if v, ok := imgAsset.Metadata["page"]; ok {
						switch t := v.(type) {
						case int:
							if t > 0 {
								page = t
							}
						case int64:
							if t > 0 {
								page = int(t)
							}
						case float64:
							if t > 0 {
								page = int(t)
							}
						case float32:
							if t > 0 {
								page = int(t)
							}
						case string:
							if n, err := strconv.Atoi(strings.TrimSpace(t)); err == nil && n > 0 {
								page = n
							}
						}
					}
				}
				if i < capN {
					noteSegs, warn, err := s.captionAssetToSegments(gctx, "figure_notes", imgAsset, page, nil, nil)
					if err != nil {
						capMu.Lock()
						capWarnings = append(capWarnings, fmt.Sprintf("caption page %d failed: %v", page, err))
						capMu.Unlock()
						return nil
					}
					if warn != "" {
						capMu.Lock()
						capWarnings = append(capWarnings, warn)
						capMu.Unlock()
					}
					segsByPage[i] = noteSegs
				}
				if i < conceptN {
					conceptSegs, warn, err := s.captionAssetToConceptSegments(gctx, imgAsset, page, nil, nil)
					if err != nil {
						capMu.Lock()
						capWarnings = append(capWarnings, fmt.Sprintf("diagram concepts page %d failed: %v", page, err))
						capMu.Unlock()
						return nil
					}
					if warn != "" {
						capMu.Lock()
						capWarnings = append(capWarnings, warn)
						capMu.Unlock()
					}
					conceptsByPage[i] = conceptSegs
				}
				return nil
			})
		}

		if err := g.Wait(); err != nil && gctx.Err() != nil {
			return segs, assets, warnings, diag, err
		}
		for i := 0; i < capN; i++ {
			segs = append(segs, segsByPage[i]...)
		}
		for i := 0; i < conceptN; i++ {
			segs = append(segs, conceptsByPage[i]...)
		}
		if len(capWarnings) > 0 {
			warnings = append(warnings, capWarnings...)
		}
	}

	if hint := outline.FromSegments(mf.OriginalName, segs, outline.MaxSections()); hint != nil {
		segs = outline.AnnotateSegmentsWithOutline(segs, hint)
		outline.ApplyHint(diag, hint)
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

	renderSequential := func() ([]AssetRef, []AssetRef, []string, map[string]any) {
		paths, err := s.ex.Media.RenderPDFToImages(ctx, pdfPath, tmpDir, localmedia.PDFRenderOptions{
			DPI:       200,
			Format:    "png",
			FirstPage: 0,
			LastPage:  0,
		})
		if err != nil {
			return nil, nil, []string{"pdf render failed: " + err.Error()}, diag
		}

		if s.ex.MaxPDFPagesRender > 0 && len(paths) > s.ex.MaxPDFPagesRender {
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

	pageCount, err := s.ex.Media.CountPDFPages(ctx, pdfPath)
	if err != nil || pageCount <= 0 {
		if err != nil {
			warnings = append(warnings, "pdf page count failed (fallback to sequential): "+err.Error())
			diag["page_count_error"] = err.Error()
		}
		return renderSequential()
	}
	diag["pages_total"] = pageCount

	if s.ex.MaxPDFPagesRender > 0 && pageCount > s.ex.MaxPDFPagesRender {
		warnings = append(warnings, fmt.Sprintf("pdf pages truncated: rendered %d capped to %d", pageCount, s.ex.MaxPDFPagesRender))
		pageCount = s.ex.MaxPDFPagesRender
	}
	if pageCount <= 0 {
		diag["pages_rendered"] = 0
		return nil, nil, warnings, diag
	}

	maxConc := runtime.NumCPU()
	if maxConc < 1 {
		maxConc = 1
	}

	assets := make([]AssetRef, pageCount)
	present := make([]bool, pageCount)
	var mu sync.Mutex

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConc)

	for page := 1; page <= pageCount; page++ {
		page := page
		g.Go(func() error {
			if err := gctx.Err(); err != nil {
				return err
			}
			pth, err := s.ex.Media.RenderPDFPage(gctx, pdfPath, tmpDir, page, localmedia.PDFRenderOptions{
				DPI:    200,
				Format: "png",
			})
			if err != nil {
				mu.Lock()
				warnings = append(warnings, fmt.Sprintf("render page %d failed: %v", page, err))
				mu.Unlock()
				return nil
			}

			key := fmt.Sprintf("%s/derived/pages/page_%04d.png", mf.StorageKey, page)
			if err := s.ex.UploadLocalToGCS(dbctx.Context{Ctx: gctx}, key, pth); err != nil {
				mu.Lock()
				warnings = append(warnings, fmt.Sprintf("upload page %d failed: %v", page, err))
				mu.Unlock()
				return nil
			}

			asset := AssetRef{
				Kind: "pdf_page",
				Key:  key,
				URL:  s.ex.Bucket.GetPublicURL(gcp.BucketCategoryMaterial, key),
				Metadata: map[string]any{
					"page":   page,
					"format": "png",
				},
			}
			assets[page-1] = asset
			present[page-1] = true
			return nil
		})
	}

	if err := g.Wait(); err != nil && gctx.Err() != nil {
		warnings = append(warnings, "render canceled: "+err.Error())
	}

	pageAssets := make([]AssetRef, 0, pageCount)
	for i := 0; i < pageCount; i++ {
		if present[i] {
			pageAssets = append(pageAssets, assets[i])
		}
	}

	diag["pages_rendered"] = len(pageAssets)
	return pageAssets, pageAssets, warnings, diag
}

func clampIntCeiling(v, min, ceiling int) int {
	if v < min {
		v = min
	}
	if ceiling > 0 && v > ceiling {
		v = ceiling
	}
	return v
}

// keep identical imports usage
var _ = strings.TrimSpace
