package steps

import (
	"context"
	"fmt"
	"strings"

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

type IngestChunksOutput struct {
	PathID              uuid.UUID `json:"path_id"`
	FilesTotal          int       `json:"files_total"`
	FilesProcessed      int       `json:"files_processed"`
	FilesAlreadyChunked int       `json:"files_already_chunked"`
}

func IngestChunks(ctx context.Context, deps IngestChunksDeps, in IngestChunksInput) (IngestChunksOutput, error) {
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
		if mf == nil || mf.ID == uuid.Nil {
			continue
		}
		if hasChunks[mf.ID] {
			out.FilesAlreadyChunked++
			continue
		}

		mf := mf
		if err := deps.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			// Contract: every job derives path_id via bootstrap.
			pathID, err := deps.Bootstrap.EnsurePath(ctx, tx, in.OwnerUserID, in.MaterialSetID)
			if err != nil {
				return err
			}
			if out.PathID == uuid.Nil {
				out.PathID = pathID
			}

			// Idempotency guard (re-check in-tx)
			chs, err := deps.Chunks.GetByMaterialFileIDs(ctx, tx, []uuid.UUID{mf.ID})
			if err != nil {
				return err
			}
			if len(chs) > 0 {
				return nil
			}

			// Conservative compensation: delete derived prefix (never the original upload key).
			if strings.TrimSpace(mf.StorageKey) != "" {
				derivedPrefix := strings.TrimRight(mf.StorageKey, "/") + "/derived/"
				if err := deps.Saga.AppendAction(ctx, tx, in.SagaID, services.SagaActionKindGCSDeletePrefix, map[string]any{
					"category": "material",
					"prefix":   derivedPrefix,
				}); err != nil {
					return err
				}
			}

			summary, err := deps.Extract.ExtractAndPersist(ctx, tx, mf)
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
					if err := deps.Saga.AppendAction(ctx, tx, in.SagaID, services.SagaActionKindGCSDeleteKey, map[string]any{
						"category": "material",
						"key":      a.Key,
					}); err != nil {
						return err
					}
				}
			}

			return nil
		}); err != nil {
			return out, err
		}

		out.FilesProcessed++
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
