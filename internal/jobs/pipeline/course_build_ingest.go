package pipelines

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/yungbote/neurobridge-backend/internal/services"
)

func (p *CourseBuildPipeline) stageIngest(buildCtx *buildContext) error {
	if buildCtx == nil {
		return nil
	}
	if p.extractor == nil {
		return fmt.Errorf("ContentExtractionService not wired on CourseBuildPipeline")
	}

	p.progress(buildCtx, "ingest", 5, "Ensuring extracted chunks exist")

	// Load existing chunks (idempotency)
	existingChunks, err := p.chunkRepo.GetByMaterialFileIDs(buildCtx.ctx, nil, buildCtx.fileIDs)
	if err != nil {
		return fmt.Errorf("load existing chunks: %w", err)
	}
	hasChunks := map[uuid.UUID]bool{}
	for _, ch := range existingChunks {
		if ch != nil && ch.MaterialFileID != uuid.Nil {
			hasChunks[ch.MaterialFileID] = true
		}
	}

	// For each file missing chunks: call the full extraction pipeline
	for i, mf := range buildCtx.files {
		if mf == nil || mf.ID == uuid.Nil {
			continue
		}
		if hasChunks[mf.ID] {
			continue
		}

		// This performs:
		// - download original from GCS
		// - DOCX/PPTX -> PDF (libreoffice)
		// - PDF pages -> images (pdftoppm)
		// - DocAI (tables/forms/layout)
		// - Vision OCR fallback
		// - Speech transcript / VideoAI (if video/audio)
		// - Caption provider for diagrams/images/frames
		// - persists segments as material_chunk rows (with provenance in metadata)
		_, exErr := p.extractor.ExtractAndPersist(buildCtx.ctx, nil, mf)
		if exErr != nil {
			// IMPORTANT: don't kill the whole course build because one file is messy.
			// Keep going; downstream will still have chunks for other files.
			p.log.Warn("Content extraction failed for material file; continuing",
				"material_file_id", mf.ID,
				"original_name", mf.OriginalName,
				"mime_type", mf.MimeType,
				"error", exErr.Error(),
			)
		}

		// mimic old progress slope in ingest: 5 -> 25
		pct := 5 + int(float64(i+1)/float64(max(1, len(buildCtx.files)))*20.0)
		p.progress(buildCtx, "ingest", pct, fmt.Sprintf("Processed %s", mf.OriginalName))

		// small yield / keep responsive
		_ = time.Now()
	}

	// Reload chunks after ensuring existence
	chunks, err := p.chunkRepo.GetByMaterialFileIDs(buildCtx.ctx, nil, buildCtx.fileIDs)
	if err != nil {
		return fmt.Errorf("load chunks after ingest: %w", err)
	}
	if len(chunks) == 0 {
		return fmt.Errorf("no chunks available after ingest")
	}
	buildCtx.chunks = chunks

	return nil
}










