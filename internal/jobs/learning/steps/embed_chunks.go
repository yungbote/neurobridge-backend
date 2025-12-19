package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/clients/openai"
	pc "github.com/yungbote/neurobridge-backend/internal/clients/pinecone"
	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/learning/index"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
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
	pathID, err := deps.Bootstrap.EnsurePath(ctx, nil, in.OwnerUserID, in.MaterialSetID)
	if err != nil {
		return out, err
	}
	out.PathID = pathID

	files, err := deps.Files.GetByMaterialSetID(ctx, nil, in.MaterialSetID)
	if err != nil {
		return out, err
	}
	fileIDs := make([]uuid.UUID, 0, len(files))
	for _, f := range files {
		if f != nil && f.ID != uuid.Nil {
			fileIDs = append(fileIDs, f.ID)
		}
	}
	chunks, err := deps.Chunks.GetByMaterialFileIDs(ctx, nil, fileIDs)
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

	const batchSize = 64
	ns := index.ChunksNamespace(in.MaterialSetID)

	for start := 0; start < len(missing); start += batchSize {
		end := start + batchSize
		if end > len(missing) {
			end = len(missing)
		}
		batch := missing[start:end]

		texts := make([]string, 0, len(batch))
		for _, ch := range batch {
			texts = append(texts, ch.Text)
		}

		vecs, err := deps.AI.Embed(ctx, texts)
		if err != nil {
			return out, err
		}
		if len(vecs) != len(batch) {
			return out, fmt.Errorf("embed_chunks: embedding count mismatch (got %d want %d)", len(vecs), len(batch))
		}

		ids := make([]string, 0, len(batch))
		pv := make([]pc.Vector, 0, len(batch))

		// 1) Write embeddings to Postgres + append compensations in the same tx.
		if err := deps.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			// EnsurePath inside the same tx (no-op if already set).
			if _, err := deps.Bootstrap.EnsurePath(ctx, tx, in.OwnerUserID, in.MaterialSetID); err != nil {
				return err
			}

			for i, ch := range batch {
				b, _ := json.Marshal(vecs[i])
				if err := deps.Chunks.UpdateFields(ctx, tx, ch.ID, map[string]interface{}{
					"embedding": datatypes.JSON(b),
				}); err != nil {
					return err
				}

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
				if err := deps.Saga.AppendAction(ctx, tx, in.SagaID, services.SagaActionKindPineconeDeleteIDs, map[string]any{
					"namespace": ns,
					"ids":       ids,
				}); err != nil {
					return err
				}
			}

			return nil
		}); err != nil {
			return out, err
		}

		out.ChunksEmbedded += len(batch)

		// 2) Upsert to Pinecone (best-effort, retrieval cache only).
		if deps.Vec == nil {
			out.PineconeSkipped = true
			continue
		}
		if len(pv) > 0 {
			if err := deps.Vec.Upsert(ctx, ns, pv); err != nil {
				out.PineconeSkipped = true
				deps.Log.Warn("pinecone upsert failed (continuing)", "namespace", ns, "err", err.Error())
				continue
			}
			out.PineconeUpserts++
		}
	}

	return out, nil
}
