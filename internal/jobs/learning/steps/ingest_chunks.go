package steps

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	ingestion "github.com/yungbote/neurobridge-backend/internal/ingestion/pipeline"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type IngestChunksDeps struct {
	DB        *gorm.DB
	Log       *logger.Logger
	Files     repos.MaterialFileRepo
	Chunks    repos.MaterialChunkRepo
	Extract   ingestion.ContentExtractionService
	Saga      services.SagaService
	Bootstrap services.LearningBuildBootstrapService
}

type IngestChunksInput struct {
	OwnerUserID   uuid.UUID
	MaterialSetID uuid.UUID
	SagaID        uuid.UUID
}

type IngestChunksOptions struct {
	// FileTimeout hard-limits time spent ingesting a single material_file.
	// If <= 0, defaults to 10 minutes.
	FileTimeout time.Duration

	// Report is an optional progress callback (e.g. to feed a heartbeat ticker).
	Report func(stage string, pct int, message string)
}

type IngestChunksOutput struct {
	PathID              uuid.UUID `json:"path_id"`
	FilesTotal          int       `json:"files_total"`
	FilesProcessed      int       `json:"files_processed"`
	FilesAlreadyChunked int       `json:"files_already_chunked"`
}

func IngestChunks(ctx context.Context, deps IngestChunksDeps, in IngestChunksInput, opts ...IngestChunksOptions) (IngestChunksOutput, error) {
	out := IngestChunksOutput{}
	if deps.DB == nil || deps.Log == nil || deps.Files == nil || deps.Chunks == nil || deps.Extract == nil || deps.Saga == nil || deps.Bootstrap == nil {
		return out, fmt.Errorf("ingest_chunks: missing deps")
	}
	if in.OwnerUserID == uuid.Nil {
		return out, fmt.Errorf("ingest_chunks: missing owner_user_id")
	}
	if in.MaterialSetID == uuid.Nil {
		return out, fmt.Errorf("ingest_chunks: missing material_set_id")
	}
	if in.SagaID == uuid.Nil {
		return out, fmt.Errorf("ingest_chunks: missing saga_id")
	}

	var opt IngestChunksOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
	fileTimeout := opt.FileTimeout
	if fileTimeout <= 0 {
		fileTimeout = 10 * time.Minute
	}
	report := opt.Report
	if report == nil {
		report = func(string, int, string) {}
	}

	files, err := deps.Files.GetByMaterialSetID(ctx, nil, in.MaterialSetID)
	if err != nil {
		return out, err
	}
	out.FilesTotal = len(files)
	if len(files) == 0 {
		return out, fmt.Errorf("ingest_chunks: no material files for set")
	}

	fileIDs := make([]uuid.UUID, 0, len(files))
	for _, f := range files {
		if f != nil && f.ID != uuid.Nil {
			fileIDs = append(fileIDs, f.ID)
		}
	}

	existing, err := deps.Chunks.GetByMaterialFileIDs(ctx, nil, fileIDs)
	if err != nil {
		return out, err
	}
	hasChunks := map[uuid.UUID]bool{}
	for _, ch := range existing {
		if ch != nil && ch.MaterialFileID != uuid.Nil {
			hasChunks[ch.MaterialFileID] = true
		}
	}

	for _, mf := range files {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		if mf == nil || mf.ID == uuid.Nil {
			continue
		}
		if hasChunks[mf.ID] {
			out.FilesAlreadyChunked++
			done := out.FilesAlreadyChunked + out.FilesProcessed
			report("ingest", ingestProgress(done, out.FilesTotal), fmt.Sprintf("Already chunked %d/%d", done, out.FilesTotal))
			continue
		}

		mf := mf
		doneBefore := out.FilesAlreadyChunked + out.FilesProcessed
		report("ingest", ingestProgress(doneBefore, out.FilesTotal), fmt.Sprintf("Ingesting %d/%d: %s", doneBefore+1, out.FilesTotal, mf.OriginalName))

		fileCtx, cancel := context.WithTimeout(ctx, fileTimeout)
		err := deps.DB.WithContext(fileCtx).Transaction(func(tx *gorm.DB) error {
			// Contract: every job derives path_id via bootstrap.
			pathID, err := deps.Bootstrap.EnsurePath(fileCtx, tx, in.OwnerUserID, in.MaterialSetID)
			if err != nil {
				return err
			}
			if out.PathID == uuid.Nil {
				out.PathID = pathID
			}

			// Idempotency guard (re-check in-tx)
			chs, err := deps.Chunks.GetByMaterialFileIDs(fileCtx, tx, []uuid.UUID{mf.ID})
			if err != nil {
				return err
			}
			if len(chs) > 0 {
				return nil
			}

			// Conservative compensation: delete derived prefix (never the original upload key).
			if strings.TrimSpace(mf.StorageKey) != "" {
				derivedPrefix := strings.TrimRight(mf.StorageKey, "/") + "/derived/"
				if err := deps.Saga.AppendAction(fileCtx, tx, in.SagaID, services.SagaActionKindGCSDeletePrefix, map[string]any{
					"category": "material",
					"prefix":   derivedPrefix,
				}); err != nil {
					return err
				}
			}

			summary, err := deps.Extract.ExtractAndPersist(fileCtx, tx, mf)
			if err != nil {
				return err
			}

			// Best-effort more specific keys (still safe if duplicates accumulate).
			if summary != nil {
				for _, a := range summary.Assets {
					if strings.TrimSpace(a.Key) == "" {
						continue
					}
					if a.Kind == "original" || a.Key == mf.StorageKey {
						continue
					}
					if err := deps.Saga.AppendAction(fileCtx, tx, in.SagaID, services.SagaActionKindGCSDeleteKey, map[string]any{
						"category": "material",
						"key":      a.Key,
					}); err != nil {
						return err
					}
				}
			}

			return nil
		})
		ctxErr := fileCtx.Err()
		cancel()
		if err != nil {
			// Prefer the context error so callers see a clear timeout/cancel.
			if errors.Is(ctxErr, context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
				return out, fmt.Errorf(
					"ingest_chunks: file timeout after %s (material_file_id=%s name=%q): %w",
					fileTimeout.String(),
					mf.ID.String(),
					mf.OriginalName,
					err,
				)
			}
			if errors.Is(ctxErr, context.Canceled) || errors.Is(err, context.Canceled) {
				return out, fmt.Errorf(
					"ingest_chunks: file canceled (material_file_id=%s name=%q): %w",
					mf.ID.String(),
					mf.OriginalName,
					err,
				)
			}
			return out, fmt.Errorf("ingest_chunks: extract failed (material_file_id=%s name=%q): %w", mf.ID.String(), mf.OriginalName, err)
		}

		out.FilesProcessed++
		done := out.FilesAlreadyChunked + out.FilesProcessed
		report("ingest", ingestProgress(done, out.FilesTotal), fmt.Sprintf("Processed %d/%d", done, out.FilesTotal))
	}

	after, err := deps.Chunks.GetByMaterialFileIDs(ctx, nil, fileIDs)
	if err != nil {
		return out, err
	}
	hasChunks = map[uuid.UUID]bool{}
	for _, ch := range after {
		if ch != nil && ch.MaterialFileID != uuid.Nil {
			hasChunks[ch.MaterialFileID] = true
		}
	}
	missing := 0
	for _, fid := range fileIDs {
		if !hasChunks[fid] {
			missing++
		}
	}
	if missing > 0 {
		return out, fmt.Errorf("ingest_chunks: chunks missing for %d files", missing)
	}

	return out, nil
}

func ingestProgress(done, total int) int {
	if total <= 0 {
		return 2
	}
	// Keep progress monotonic and <100 until job Succeed.
	const (
		min = 2
		max = 95
	)
	if done < 0 {
		done = 0
	}
	if done > total {
		done = total
	}
	pct := min + int(float64(done)/float64(total)*float64(max-min))
	if pct < min {
		return min
	}
	if pct > max {
		return max
	}
	return pct
}
