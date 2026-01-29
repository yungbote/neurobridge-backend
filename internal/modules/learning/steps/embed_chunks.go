package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/materialsetctx"
	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/index"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
	pc "github.com/yungbote/neurobridge-backend/internal/platform/pinecone"
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
	PathID        uuid.UUID
}

type EmbedChunksOutput struct {
	PathID          uuid.UUID `json:"path_id"`
	ChunksTotal     int       `json:"chunks_total"`
	ChunksEmbedded  int       `json:"chunks_embedded"`
	PineconeUpserts int       `json:"pinecone_upserts"`
	PineconeSkipped bool      `json:"pinecone_skipped"`
	Adaptive        map[string]any `json:"adaptive,omitempty"`
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
	pathID, err := resolvePathID(ctx, deps.Bootstrap, in.OwnerUserID, in.MaterialSetID, in.PathID)
	if err != nil {
		return out, err
	}
	out.PathID = pathID

	adaptiveEnabled := adaptiveParamsEnabledForStage("embed_chunks")
	signals := AdaptiveSignals{}
	if adaptiveEnabled {
		signals = loadAdaptiveSignals(ctx, deps.DB, in.MaterialSetID, pathID)
	}
	adaptiveParams := map[string]any{}
	defer func() {
		if deps.Log != nil && adaptiveEnabled && len(adaptiveParams) > 0 {
			deps.Log.Info("embed_chunks: adaptive params", "adaptive", adaptiveStageMeta("embed_chunks", adaptiveEnabled, signals, adaptiveParams))
		}
		out.Adaptive = adaptiveStageMeta("embed_chunks", adaptiveEnabled, signals, adaptiveParams)
	}()

	// Derived material sets share the underlying chunk vectors namespace with their source upload batch.
	setCtx, err := materialsetctx.Resolve(ctx, deps.DB, in.MaterialSetID)
	if err != nil {
		return out, err
	}
	sourceSetID := setCtx.SourceMaterialSetID

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

	batchSizeCeiling := envInt("EMBED_CHUNKS_BATCH_SIZE", 128)
	batchSize := batchSizeCeiling
	if adaptiveEnabled {
		batchSize = clampIntCeiling(int(math.Round(float64(signals.ChunkCount)/6.0)), 64, batchSizeCeiling)
	}
	adaptiveParams["EMBED_CHUNKS_BATCH_SIZE"] = map[string]any{
		"actual":  batchSize,
		"ceiling": batchSizeCeiling,
	}
	if batchSize < 8 {
		batchSize = 8
	}
	if batchSize > 256 {
		batchSize = 256
	}
	maxTokens := envIntAllowZero("EMBED_CHUNKS_MAX_TOKENS", 7000)
	if maxTokens < 0 {
		maxTokens = 0
	}
	adaptiveParams["EMBED_CHUNKS_MAX_TOKENS"] = map[string]any{"actual": maxTokens}
	maxConc := envInt("EMBED_CHUNKS_CONCURRENCY", 6)
	if maxConc < 1 {
		maxConc = 1
	}
	adaptiveParams["EMBED_CHUNKS_CONCURRENCY"] = map[string]any{"actual": maxConc}

	ns := index.ChunksNamespace(sourceSetID)

	type embedItem struct {
		Chunk  *types.MaterialChunk
		Text   string
		Tokens int
	}
	type embedBatch struct {
		Items    []embedItem
		Oversize bool
	}

	items := make([]embedItem, 0, len(missing))
	for _, ch := range missing {
		txt := chunkTextForEmbedding(ch)
		items = append(items, embedItem{
			Chunk:  ch,
			Text:   txt,
			Tokens: estimateTokens(txt),
		})
	}

	batches := make([]embedBatch, 0, (len(items)/maxInt(batchSize, 1))+1)
	cur := make([]embedItem, 0, batchSize)
	curTokens := 0
	for _, it := range items {
		if maxTokens > 0 && it.Tokens > maxTokens {
			batches = append(batches, embedBatch{Items: []embedItem{it}, Oversize: true})
			continue
		}
		if len(cur) >= batchSize || (maxTokens > 0 && curTokens+it.Tokens > maxTokens && len(cur) > 0) {
			batches = append(batches, embedBatch{Items: cur})
			cur = make([]embedItem, 0, batchSize)
			curTokens = 0
		}
		cur = append(cur, it)
		curTokens += it.Tokens
	}
	if len(cur) > 0 {
		batches = append(batches, embedBatch{Items: cur})
	}

	var chunksEmbedded int32
	var pineconeUpserts int32
	var pineconeSkipped int32

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConc)

	for _, batch := range batches {
		batch := batch
		g.Go(func() error {
			if len(batch.Items) == 0 {
				return nil
			}

			texts := make([]string, 0, len(batch.Items))
			for _, it := range batch.Items {
				texts = append(texts, it.Text)
			}

			var vecs [][]float32
			if batch.Oversize {
				it := batch.Items[0]
				parts := splitTextByTokens(it.Text, maxTokens)
				if len(parts) == 0 {
					parts = []string{it.Text}
				}
				partVecs := make([][]float32, 0, len(parts))
				weights := make([]float64, 0, len(parts))
				for _, part := range parts {
					v, err := deps.AI.Embed(gctx, []string{part})
					if err != nil {
						return err
					}
					if len(v) != 1 {
						return fmt.Errorf("embed_chunks: embedding count mismatch (got %d want 1)", len(v))
					}
					partVecs = append(partVecs, v[0])
					weights = append(weights, float64(maxInt(estimateTokens(part), 1)))
				}
				avg := averageEmbeddingWeighted(partVecs, weights)
				if len(avg) == 0 {
					return fmt.Errorf("embed_chunks: empty embedding for oversize chunk")
				}
				vecs = [][]float32{avg}
				if deps.Log != nil && len(parts) > 1 {
					deps.Log.Warn("embed_chunks: split oversize chunk for embedding", "parts", len(parts), "tokens", it.Tokens)
				}
			} else {
				var err error
				vecs, err = deps.AI.Embed(gctx, texts)
				if err != nil {
					return err
				}
			}
			if len(vecs) != len(batch.Items) {
				return fmt.Errorf("embed_chunks: embedding count mismatch (got %d want %d)", len(vecs), len(batch.Items))
			}

			ids := make([]string, 0, len(batch.Items))
			pv := make([]pc.Vector, 0, len(batch.Items))

			// 1) Write embeddings to Postgres + append compensations in the same tx.
			if err := deps.DB.WithContext(gctx).Transaction(func(tx *gorm.DB) error {
				dbc := dbctx.Context{Ctx: gctx, Tx: tx}
				// Bulk update is significantly faster than per-row updates.
				batchChunks := make([]*types.MaterialChunk, 0, len(batch.Items))
				for _, it := range batch.Items {
					batchChunks = append(batchChunks, it.Chunk)
				}
				if err := bulkUpdateChunkEmbeddings(dbc, batchChunks, vecs); err != nil {
					return err
				}

				for i, it := range batch.Items {
					ch := it.Chunk
					id := ch.ID.String()
					ids = append(ids, id)

					// Prepare Pinecone vector (upsert is after commit).
					pv = append(pv, pc.Vector{
						ID:     id,
						Values: vecs[i],
						Metadata: map[string]any{
							"type":             "chunk",
							"material_set_id":  sourceSetID.String(),
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

			atomic.AddInt32(&chunksEmbedded, int32(len(batch.Items)))

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

func averageEmbeddingWeighted(vecs [][]float32, weights []float64) []float32 {
	if len(vecs) == 0 {
		return nil
	}
	dim := len(vecs[0])
	if dim == 0 {
		return nil
	}
	acc := make([]float64, dim)
	var total float64
	for i, v := range vecs {
		if len(v) != dim {
			continue
		}
		w := 1.0
		if i < len(weights) && weights[i] > 0 {
			w = weights[i]
		}
		total += w
		for j, f := range v {
			acc[j] += float64(f) * w
		}
	}
	if total <= 0 {
		out := make([]float32, dim)
		copy(out, vecs[0])
		return out
	}
	out := make([]float32, dim)
	for i := range acc {
		out[i] = float32(acc[i] / total)
	}
	return out
}
