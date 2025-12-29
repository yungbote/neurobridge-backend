package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/clients/openai"
	pc "github.com/yungbote/neurobridge-backend/internal/clients/pinecone"
	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/learning/index"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
	"golang.org/x/sync/errgroup"
)

type EmbedChunksDeps struct {
	DB        *gorm.DB
	Log       *logger.Logger
	Files     repos.MaterialFileRepo
	Chunks    repos.MaterialChunkRepo
	AI        openai.Client
	Vec       pc.VectorStore
	Saga      services.SagaService
	Bootstrap services.LearningBuildBootstrapService
}

type EmbedChunksInput struct {
	OwnerUserID   uuid.UUID
	MaterialSetID uuid.UUID
	SagaID        uuid.UUID
}

type EmbedChunksOutput struct {
	PathID          uuid.UUID `json:"path_id"`
	ChunksTotal     int       `json:"chunks_total"`
	ChunksEmbedded  int       `json:"chunks_embedded"`
	PineconeUpserts int       `json:"pinecone_upserts"`
	PineconeSkipped bool      `json:"pinecone_skipped"`
}

func EmbedChunks(ctx context.Context, deps EmbedChunksDeps, in EmbedChunksInput) (EmbedChunksOutput, error) {
	out := EmbedChunksOutput{}
	if deps.DB == nil || deps.Log == nil || deps.Files == nil || deps.Chunks == nil || deps.AI == nil || deps.Bootstrap == nil || deps.Saga == nil {
		return out, fmt.Errorf("embed_chunks: missing deps")
	}
	if in.OwnerUserID == uuid.Nil {
		return out, fmt.Errorf("embed_chunks: missing owner_user_id")
	}
	if in.MaterialSetID == uuid.Nil {
		return out, fmt.Errorf("embed_chunks: missing material_set_id")
	}
	if in.SagaID == uuid.Nil {
		return out, fmt.Errorf("embed_chunks: missing saga_id")
	}

	// Contract: derive/ensure path_id (even if this stage doesn't use it further).
	pathID, err := deps.Bootstrap.EnsurePath(dbctx.Context{Ctx: ctx}, in.OwnerUserID, in.MaterialSetID)
	if err != nil {
		return out, err
	}
	out.PathID = pathID

	files, err := deps.Files.GetByMaterialSetID(dbctx.Context{Ctx: ctx}, in.MaterialSetID)
	if err != nil {
		return out, err
	}
	fileIDs := make([]uuid.UUID, 0, len(files))
	for _, f := range files {
		if f != nil && f.ID != uuid.Nil {
			fileIDs = append(fileIDs, f.ID)
		}
	}
	chunks, err := deps.Chunks.GetByMaterialFileIDs(dbctx.Context{Ctx: ctx}, fileIDs)
	if err != nil {
		return out, err
	}
	out.ChunksTotal = len(chunks)
	if len(chunks) == 0 {
		return out, fmt.Errorf("embed_chunks: no chunks exist to embed")
	}

	missing := make([]*types.MaterialChunk, 0)
	for _, ch := range chunks {
		if ch == nil || ch.ID == uuid.Nil {
			continue
		}
		if embeddingMissing(ch.Embedding) {
			missing = append(missing, ch)
		}
	}
	if len(missing) == 0 {
		return out, nil
	}

	batchSize := envInt("EMBED_CHUNKS_BATCH_SIZE", 64)
	if batchSize < 8 {
		batchSize = 8
	}
	if batchSize > 256 {
		batchSize = 256
	}
	maxConc := envInt("EMBED_CHUNKS_CONCURRENCY", 2)
	if maxConc < 1 {
		maxConc = 1
	}
	if maxConc > 8 {
		maxConc = 8
	}

	ns := index.ChunksNamespace(in.MaterialSetID)

	var chunksEmbedded int32
	var pineconeUpserts int32
	var pineconeSkipped int32

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConc)

	for start := 0; start < len(missing); start += batchSize {
		end := start + batchSize
		if end > len(missing) {
			end = len(missing)
		}
		batch := missing[start:end]

		g.Go(func() error {
			texts := make([]string, 0, len(batch))
			for _, ch := range batch {
				texts = append(texts, ch.Text)
			}

			vecs, err := deps.AI.Embed(gctx, texts)
			if err != nil {
				return err
			}
			if len(vecs) != len(batch) {
				return fmt.Errorf("embed_chunks: embedding count mismatch (got %d want %d)", len(vecs), len(batch))
			}

			ids := make([]string, 0, len(batch))
			pv := make([]pc.Vector, 0, len(batch))

			// 1) Write embeddings to Postgres + append compensations in the same tx.
			if err := deps.DB.WithContext(gctx).Transaction(func(tx *gorm.DB) error {
				dbc := dbctx.Context{Ctx: gctx, Tx: tx}
				// Bulk update is significantly faster than per-row updates.
				if err := bulkUpdateChunkEmbeddings(dbc, batch, vecs); err != nil {
					return err
				}

				for i, ch := range batch {
					id := ch.ID.String()
					ids = append(ids, id)

					// Prepare Pinecone vector (upsert is after commit).
					pv = append(pv, pc.Vector{
						ID:     id,
						Values: vecs[i],
						Metadata: map[string]any{
							"type":             "chunk",
							"material_set_id":  in.MaterialSetID.String(),
							"material_file_id": ch.MaterialFileID.String(),
							"chunk_id":         id,
							"index":            ch.Index,
							"kind":             strings.TrimSpace(ch.Kind),
							"provider":         strings.TrimSpace(ch.Provider),
						},
					})
				}

				if deps.Vec != nil && len(ids) > 0 {
					if err := deps.Saga.AppendAction(dbc, in.SagaID, services.SagaActionKindPineconeDeleteIDs, map[string]any{
						"namespace": ns,
						"ids":       ids,
					}); err != nil {
						return err
					}
				}

				return nil
			}); err != nil {
				return err
			}

			atomic.AddInt32(&chunksEmbedded, int32(len(batch)))

			// 2) Upsert to Pinecone (best-effort, retrieval cache only).
			if deps.Vec == nil {
				atomic.StoreInt32(&pineconeSkipped, 1)
				return nil
			}
			if len(pv) > 0 {
				if err := deps.Vec.Upsert(gctx, ns, pv); err != nil {
					atomic.StoreInt32(&pineconeSkipped, 1)
					deps.Log.Warn("pinecone upsert failed (continuing)", "namespace", ns, "err", err.Error())
					return nil
				}
				atomic.AddInt32(&pineconeUpserts, 1)
			}

			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return out, err
	}

	out.ChunksEmbedded = int(atomic.LoadInt32(&chunksEmbedded))
	out.PineconeUpserts = int(atomic.LoadInt32(&pineconeUpserts))
	out.PineconeSkipped = atomic.LoadInt32(&pineconeSkipped) == 1

	return out, nil
}

func bulkUpdateChunkEmbeddings(dbc dbctx.Context, batch []*types.MaterialChunk, vecs [][]float32) error {
	if dbc.Tx == nil {
		return fmt.Errorf("bulkUpdateChunkEmbeddings: tx required")
	}
	if len(batch) == 0 {
		return nil
	}
	if len(vecs) != len(batch) {
		return fmt.Errorf("bulkUpdateChunkEmbeddings: embedding count mismatch (got %d want %d)", len(vecs), len(batch))
	}

	now := time.Now().UTC()

	var (
		caseParts []string
		args      []any
		ids       []uuid.UUID
	)
	caseParts = append(caseParts, "CASE id")
	for i, ch := range batch {
		if ch == nil || ch.ID == uuid.Nil {
			continue
		}
		b, _ := json.Marshal(vecs[i])
		caseParts = append(caseParts, "WHEN ? THEN ?::jsonb")
		args = append(args, ch.ID, string(b))
		ids = append(ids, ch.ID)
	}
	caseParts = append(caseParts, "ELSE embedding")
	caseParts = append(caseParts, "END")

	if len(ids) == 0 {
		return nil
	}

	query := fmt.Sprintf(
		`UPDATE material_chunk
		 SET embedding = %s,
		     updated_at = ?
		 WHERE id IN ?`,
		strings.Join(caseParts, " "),
	)
	args = append(args, now, ids)
	return dbc.Tx.WithContext(dbc.Ctx).Exec(query, args...).Error
}
