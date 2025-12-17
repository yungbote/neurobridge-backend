package pipeline

import (
	"context"
	"os"
	"strings"

	"github.com/yungbote/neurobridge-backend/internal/ingestion/extractor"
	"github.com/yungbote/neurobridge-backend/internal/types"
)

func (s *service) handleOffice(ctx context.Context, mf *types.MaterialFile, officePath string, kind string) ([]Segment, []AssetRef, []string, map[string]any, error) {
	diag := map[string]any{"pipeline": "office", "kind": kind}
	var warnings []string
	var assets []AssetRef
	var segs []Segment

	text, warn, nd := s.ex.BestEffortNativeText(mf.OriginalName, mf.MimeType, nil)
	extractor.MergeDiag(diag, nd)
	if warn != "" {
		warnings = append(warnings, warn)
	}
	if strings.TrimSpace(text) != "" {
		segs = append(segs, Segment{
			Text: text,
			Metadata: map[string]any{
				"kind":   "native_text",
				"source": kind,
			},
		})
	}

	if s.ex.Media == nil {
		return segs, assets, append(warnings, "media tools missing: cannot convert office to pdf"), diag, nil
	}

	tmpDir, err := os.MkdirTemp("", "nb_office_pdf_*")
	if err != nil {
		return segs, assets, append(warnings, "temp dir err: "+err.Error()), diag, nil
	}
	defer os.RemoveAll(tmpDir)

	pdfPath, err := s.ex.Media.ConvertOfficeToPDF(ctx, officePath, tmpDir)
	if err != nil {
		return segs, assets, append(warnings, "office->pdf failed: "+err.Error()), diag, nil
	}

	pdfSegs, pdfAssets, pdfWarn, pdfDiag, _ := s.handlePDF(ctx, mf, pdfPath)
	segs = append(segs, pdfSegs...)
	assets = append(assets, pdfAssets...)
	warnings = append(warnings, pdfWarn...)
	extractor.MergeDiag(diag, pdfDiag)

	return segs, assets, warnings, diag, nil
}










