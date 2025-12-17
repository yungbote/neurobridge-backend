package pipelines

import (
	"fmt"
	"time"

	"github.com/google/uuid"
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

	totalFiles := max(1, len(buildCtx.files))
	processed := 0

	// For each file missing chunks: call the full extraction pipeline
	for _, mf := range buildCtx.files {
		if mf == nil || mf.ID == uuid.Nil {
			continue
		}
		if hasChunks[mf.ID] {
			processed++
			// still advance progress based on real count
			pct := 5 + int(float64(processed)/float64(totalFiles)*20.0)
			p.progress(buildCtx, "ingest", pct, fmt.Sprintf("Chunks already exist for %s", mf.OriginalName))
			continue
		}

		_, exErr := p.extractor.ExtractAndPersist(buildCtx.ctx, nil, mf)
		if exErr != nil {
			// Keep going; downstream may still have chunks for other files.
			p.log.Warn("Content extraction failed for material file; continuing",
				"material_file_id", mf.ID,
				"original_name", mf.OriginalName,
				"mime_type", mf.MimeType,
				"error", exErr.Error(),
			)
		}

		processed++
		pct := 5 + int(float64(processed)/float64(totalFiles)*20.0)
		p.progress(buildCtx, "ingest", pct, fmt.Sprintf("Processed %s", mf.OriginalName))

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

	// Ensure stage completes even when everything was already present
	p.progress(buildCtx, "ingest", 25, "Chunks ready")
	return nil
}










