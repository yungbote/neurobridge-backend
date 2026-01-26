package steps

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/modules/learning/graphrag"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	pc "github.com/yungbote/neurobridge-backend/internal/platform/pinecone"
	"golang.org/x/sync/errgroup"
)

type chunkRetrievePlan struct {
	MaterialSetID uuid.UUID
	ChunksNS      string

	QueryText string
	QueryEmb  []float32

	FileIDs    []uuid.UUID
	AllowFiles map[uuid.UUID]bool

	SeedK    int
	LexicalK int
	FinalK   int

	ChunkEmbs []chunkEmbedding // local fallback embeddings
}

func graphAssistedChunkIDs(ctx context.Context, db *gorm.DB, vec pc.VectorStore, plan chunkRetrievePlan) ([]uuid.UUID, map[string]any, error) {
	trace := map[string]any{}
	if ctx == nil {
		ctx = context.Background()
	}
	if plan.MaterialSetID == uuid.Nil || strings.TrimSpace(plan.ChunksNS) == "" || len(plan.QueryEmb) == 0 || plan.FinalK <= 0 {
		return nil, trace, nil
	}
	if plan.SeedK <= 0 {
		plan.SeedK = 12
	}
	if plan.SeedK > 40 {
		plan.SeedK = 40
	}
	if plan.FinalK > 80 {
		plan.FinalK = 80
	}

	seeds := make([]graphrag.SeedChunk, 0, plan.SeedK+plan.LexicalK)
	seedIDs := make([]uuid.UUID, 0, plan.SeedK+plan.LexicalK)
	seenSeed := map[uuid.UUID]bool{}

	var (
		denseSeeds []graphrag.SeedChunk
		denseIDs   []uuid.UUID
		lexSeeds   []graphrag.SeedChunk
		lexIDs     []uuid.UUID
		traceMu    sync.Mutex
	)
	setTrace := func(k string, v any) {
		traceMu.Lock()
		trace[k] = v
		traceMu.Unlock()
	}

	g, gctx := errgroup.WithContext(ctx)
	if vec != nil {
		g.Go(func() error {
			if err := gctx.Err(); err != nil {
				return err
			}
			start := time.Now()
			matches, err := vec.QueryMatches(gctx, plan.ChunksNS, plan.QueryEmb, plan.SeedK, pineconeChunkFilterWithAllowlist(plan.AllowFiles))
			setTrace("dense_ms", time.Since(start).Milliseconds())
			if err != nil {
				setTrace("dense_err", err.Error())
				return nil
			}
			setTrace("dense_count", len(matches))
			outSeeds := make([]graphrag.SeedChunk, 0, len(matches))
			outIDs := make([]uuid.UUID, 0, len(matches))
			for _, m := range matches {
				id, err := uuid.Parse(strings.TrimSpace(m.ID))
				if err != nil || id == uuid.Nil {
					continue
				}
				outIDs = append(outIDs, id)
				outSeeds = append(outSeeds, graphrag.SeedChunk{ChunkID: id, Score: m.Score})
			}
			denseSeeds = outSeeds
			denseIDs = outIDs
			return nil
		})
	}

	if db != nil && plan.LexicalK > 0 && len(plan.FileIDs) > 0 && strings.TrimSpace(plan.QueryText) != "" {
		g.Go(func() error {
			if err := gctx.Err(); err != nil {
				return err
			}
			start := time.Now()
			lex, err := lexicalChunkIDs(dbctx.Context{Ctx: gctx, Tx: db}, plan.FileIDs, plan.QueryText, plan.LexicalK)
			setTrace("lex_ms", time.Since(start).Milliseconds())
			if err != nil {
				setTrace("lex_err", err.Error())
				return nil
			}
			setTrace("lex_count", len(lex))
			outSeeds := make([]graphrag.SeedChunk, 0, len(lex))
			outIDs := make([]uuid.UUID, 0, len(lex))
			for _, id := range lex {
				if id == uuid.Nil {
					continue
				}
				outIDs = append(outIDs, id)
				outSeeds = append(outSeeds, graphrag.SeedChunk{ChunkID: id, Score: 0.35})
			}
			lexSeeds = outSeeds
			lexIDs = outIDs
			return nil
		})
	}

	if err := g.Wait(); err != nil && gctx.Err() != nil {
		return nil, trace, err
	}

	appendSeeds := func(ids []uuid.UUID, in []graphrag.SeedChunk) {
		for i := range ids {
			id := ids[i]
			if id == uuid.Nil || seenSeed[id] {
				continue
			}
			seenSeed[id] = true
			seedIDs = append(seedIDs, id)
			seeds = append(seeds, in[i])
		}
	}
	if len(denseIDs) > 0 && len(denseSeeds) == len(denseIDs) {
		appendSeeds(denseIDs, denseSeeds)
	}
	if len(lexIDs) > 0 && len(lexSeeds) == len(lexIDs) {
		appendSeeds(lexIDs, lexSeeds)
	}

	// Local dense fallback (cosine over stored embeddings).
	if len(seeds) == 0 && len(plan.ChunkEmbs) > 0 {
		start := time.Now()
		type scored struct {
			ID    uuid.UUID
			Score float64
		}
		arr := make([]scored, 0, len(plan.ChunkEmbs))
		for _, ce := range plan.ChunkEmbs {
			if ce.ID == uuid.Nil || len(ce.Emb) == 0 {
				continue
			}
			arr = append(arr, scored{ID: ce.ID, Score: cosineSim(plan.QueryEmb, ce.Emb)})
		}
		sort.Slice(arr, func(i, j int) bool { return arr[i].Score > arr[j].Score })
		if len(arr) > plan.SeedK {
			arr = arr[:plan.SeedK]
		}
		trace["dense_local_ms"] = time.Since(start).Milliseconds()
		trace["dense_local_count"] = len(arr)
		for _, s := range arr {
			if s.ID == uuid.Nil || seenSeed[s.ID] {
				continue
			}
			seenSeed[s.ID] = true
			seedIDs = append(seedIDs, s.ID)
			seeds = append(seeds, graphrag.SeedChunk{ChunkID: s.ID, Score: s.Score})
		}
	}

	trace["seed_count"] = len(seeds)
	if len(seeds) == 0 {
		return nil, trace, nil
	}

	// Graph expansion (best-effort; seed chunks always retained).
	start := time.Now()
	scores, gtrace, err := graphrag.ExpandMaterialChunkScores(ctx, db, plan.MaterialSetID, seeds, graphrag.MaterialChunkExpandOptions{
		AllowFileIDs:          plan.AllowFiles,
		MaxSeeds:              plan.SeedK,
		MaxConcepts:           45,
		MaxEntities:           30,
		MaxClaims:             30,
		MaxEvidencePerConcept: 10,
		MaxOut:                maxInt(plan.FinalK*4, 60),
	})
	trace["graph_ms"] = time.Since(start).Milliseconds()
	if err != nil {
		trace["graph_err"] = err.Error()
	}
	if len(gtrace) > 0 {
		trace["graph"] = gtrace
	}

	if len(scores) == 0 {
		// Fallback: just return deduped seed ordering.
		out := dedupeUUIDsPreserveOrder(seedIDs)
		if len(out) > plan.FinalK {
			out = out[:plan.FinalK]
		}
		return out, trace, nil
	}

	type scoredID struct {
		ID    uuid.UUID
		Score float64
	}
	ranked := make([]scoredID, 0, len(scores))
	for id, sc := range scores {
		if id == uuid.Nil || sc <= 0 {
			continue
		}
		ranked = append(ranked, scoredID{ID: id, Score: sc})
	}
	sort.Slice(ranked, func(i, j int) bool { return ranked[i].Score > ranked[j].Score })

	out := make([]uuid.UUID, 0, plan.FinalK)
	for _, r := range ranked {
		out = append(out, r.ID)
		if len(out) >= plan.FinalK {
			break
		}
	}
	return out, trace, nil
}
