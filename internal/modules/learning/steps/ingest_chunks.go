package steps

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	ingestion "github.com/yungbote/neurobridge-backend/internal/modules/learning/ingestion/pipeline"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
	"golang.org/x/sync/errgroup"
)

type IngestChunksDeps struct {
	DB        *gorm.DB
	Log       *logger.Logger
	Files     repos.MaterialFileRepo
	Chunks    repos.MaterialChunkRepo
	Extract   ingestion.ContentExtractionService
	Saga      services.SagaService
	Bootstrap services.LearningBuildBootstrapService
	Artifacts repos.LearningArtifactRepo
}

type IngestChunksInput struct {
	OwnerUserID   uuid.UUID
	MaterialSetID uuid.UUID
	SagaID        uuid.UUID
	PathID        uuid.UUID
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
	CacheHit            bool      `json:"cache_hit,omitempty"`
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

	pathID, err := resolvePathID(ctx, deps.Bootstrap, in.OwnerUserID, in.MaterialSetID, in.PathID)
	if err != nil {
		return out, err
	}
	out.PathID = pathID

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

	files, err := deps.Files.GetByMaterialSetID(dbctx.Context{Ctx: ctx}, in.MaterialSetID)
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

	existing, err := deps.Chunks.GetByMaterialFileIDs(dbctx.Context{Ctx: ctx}, fileIDs)
	if err != nil {
		return out, err
	}
	hasChunks := map[uuid.UUID]bool{}
	for _, ch := range existing {
		if ch != nil && ch.MaterialFileID != uuid.Nil {
			hasChunks[ch.MaterialFileID] = true
		}
	}
	allChunked := true
	for _, f := range files {
		if f == nil || f.ID == uuid.Nil {
			continue
		}
		if !hasChunks[f.ID] {
			allChunked = false
			break
		}
	}

	var ingestInputHash string
	if deps.Artifacts != nil && artifactCacheEnabled() {
		payload := map[string]any{
			"files": filesFingerprint(files),
			"env":   envSnapshot([]string{"INGEST_CHUNKS_", "OPENAI_VISION_"}, []string{"OPENAI_VISION_MODEL"}),
		}
		if h, err := computeArtifactHash("ingest_chunks", in.MaterialSetID, uuid.Nil, payload); err == nil {
			ingestInputHash = h
		}
		if allChunked {
			maxFiles := maxFileUpdatedAt(files)
			maxChunks := maxChunkUpdatedAt(existing)
			chunksFresh := maxChunks.IsZero() || !maxChunks.Before(maxFiles)
			if chunksFresh && ingestInputHash != "" {
				if _, hit, err := artifactCacheGet(ctx, deps.Artifacts, in.OwnerUserID, in.MaterialSetID, uuid.Nil, "ingest_chunks", ingestInputHash); err == nil && hit {
					out.FilesAlreadyChunked = len(files)
					out.CacheHit = true
					return out, nil
				}
				if artifactCacheSeedExisting() {
					_ = artifactCacheUpsert(ctx, deps.Artifacts, &types.LearningArtifact{
						OwnerUserID:   in.OwnerUserID,
						MaterialSetID: in.MaterialSetID,
						PathID:        uuid.Nil,
						ArtifactType:  "ingest_chunks",
						InputHash:     ingestInputHash,
						Version:       artifactHashVersion,
						Metadata: marshalMeta(map[string]any{
							"files_total":  len(files),
							"chunks_total": len(existing),
							"seeded":       true,
						}),
					})
					out.FilesAlreadyChunked = len(files)
					out.CacheHit = true
					return out, nil
				}
			}
		}
	}

	maxConc := envInt("INGEST_CHUNKS_FILE_CONCURRENCY", 4)
	if maxConc < 1 {
		maxConc = 1
	}

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConc)

	var (
		filesProcessed      int32
		filesAlreadyChunked int32
		reportMu            sync.Mutex
	)

	for _, mf := range files {
		mf := mf
		g.Go(func() error {
			if gctx.Err() != nil {
				return gctx.Err()
			}
			if mf == nil || mf.ID == uuid.Nil {
				return nil
			}
			if hasChunks[mf.ID] {
				atomic.AddInt32(&filesAlreadyChunked, 1)
				done := int(atomic.LoadInt32(&filesAlreadyChunked) + atomic.LoadInt32(&filesProcessed))
				reportMu.Lock()
				report("ingest", ingestProgress(done, out.FilesTotal), fmt.Sprintf("Already chunked %d/%d", done, out.FilesTotal))
				reportMu.Unlock()

				// Ensure thumbnails exist even when extraction is skipped via idempotency.
				// This fixes older/partial ingestions where chunks exist but thumbnail_asset_id is empty.
				func() {
					mt := strings.ToLower(strings.TrimSpace(mf.MimeType))
					if strings.HasPrefix(mt, "image/") {
						// Images already have a built-in thumbnail fallback (the original upload).
						return
					}
					fileCtx, cancel := context.WithTimeout(gctx, 30*time.Second)
					defer cancel()
					_ = deps.DB.WithContext(fileCtx).Transaction(func(tx *gorm.DB) error {
						return deps.Extract.EnsureThumbnail(dbctx.Context{Ctx: fileCtx, Tx: tx}, mf)
					})
				}()

				return nil
			}

			reportMu.Lock()
			doneBefore := int(atomic.LoadInt32(&filesAlreadyChunked) + atomic.LoadInt32(&filesProcessed))
			report("ingest", ingestProgress(doneBefore, out.FilesTotal), fmt.Sprintf("Ingesting %d/%d: %s", doneBefore+1, out.FilesTotal, mf.OriginalName))
			reportMu.Unlock()

			fileCtx, cancel := context.WithTimeout(gctx, fileTimeout)
			err := deps.DB.WithContext(fileCtx).Transaction(func(tx *gorm.DB) error {
				dbc := dbctx.Context{Ctx: fileCtx, Tx: tx}
				// Idempotency guard (re-check in-tx)
				chs, err := deps.Chunks.GetByMaterialFileIDs(dbc, []uuid.UUID{mf.ID})
				if err != nil {
					return err
				}
				if len(chs) > 0 {
					return nil
				}

				// Conservative compensation: delete derived prefix (never the original upload key).
				if strings.TrimSpace(mf.StorageKey) != "" {
					derivedPrefix := strings.TrimRight(mf.StorageKey, "/") + "/derived/"
					if err := deps.Saga.AppendAction(dbc, in.SagaID, services.SagaActionKindGCSDeletePrefix, map[string]any{
						"category": "material",
						"prefix":   derivedPrefix,
					}); err != nil {
						return err
					}
				}

				summary, err := deps.Extract.ExtractAndPersist(dbc, mf)
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
						if err := deps.Saga.AppendAction(dbc, in.SagaID, services.SagaActionKindGCSDeleteKey, map[string]any{
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
					return fmt.Errorf(
						"ingest_chunks: file timeout after %s (material_file_id=%s name=%q): %w",
						fileTimeout.String(),
						mf.ID.String(),
						mf.OriginalName,
						err,
					)
				}
				if errors.Is(ctxErr, context.Canceled) || errors.Is(err, context.Canceled) {
					return fmt.Errorf(
						"ingest_chunks: file canceled (material_file_id=%s name=%q): %w",
						mf.ID.String(),
						mf.OriginalName,
						err,
					)
				}
				return fmt.Errorf("ingest_chunks: extract failed (material_file_id=%s name=%q): %w", mf.ID.String(), mf.OriginalName, err)
			}

			atomic.AddInt32(&filesProcessed, 1)
			done := int(atomic.LoadInt32(&filesAlreadyChunked) + atomic.LoadInt32(&filesProcessed))
			reportMu.Lock()
			report("ingest", ingestProgress(done, out.FilesTotal), fmt.Sprintf("Processed %d/%d", done, out.FilesTotal))
			reportMu.Unlock()

			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return out, err
	}

	out.FilesProcessed = int(atomic.LoadInt32(&filesProcessed))
	out.FilesAlreadyChunked = int(atomic.LoadInt32(&filesAlreadyChunked))

	after, err := deps.Chunks.GetByMaterialFileIDs(dbctx.Context{Ctx: ctx}, fileIDs)
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

	if ingestInputHash != "" && deps.Artifacts != nil && artifactCacheEnabled() {
		_ = artifactCacheUpsert(ctx, deps.Artifacts, &types.LearningArtifact{
			OwnerUserID:   in.OwnerUserID,
			MaterialSetID: in.MaterialSetID,
			PathID:        uuid.Nil,
			ArtifactType:  "ingest_chunks",
			InputHash:     ingestInputHash,
			Version:       artifactHashVersion,
			Metadata: marshalMeta(map[string]any{
				"files_total":  len(files),
				"chunks_total": len(after),
			}),
		})
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
