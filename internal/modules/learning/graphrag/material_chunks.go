package graphrag

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
)

type SeedChunk struct {
	ChunkID uuid.UUID
	Score   float64
}

type MaterialChunkExpandOptions struct {
	// AllowFileIDs optionally restricts expansion to a subset of material_file IDs.
	AllowFileIDs map[uuid.UUID]bool

	// Limits (safety/perf).
	MaxSeeds              int
	MaxConcepts           int
	MaxEntities           int
	MaxClaims             int
	MaxEvidencePerConcept int
	MaxOut                int
}

// ExpandMaterialChunkScores performs a production-safe "graph-assisted RAG" expansion:
// start from seed chunks (typically dense retrieval), then expand via:
// - ConceptEvidence + ConceptEdge (prereq/related/analogy)
// - MaterialChunkEntity (mentions) and MaterialChunkClaim (supports)
//
// It never replaces the base retrieval: seed chunks are always retained, and expansion is best-effort.
func ExpandMaterialChunkScores(
	ctx context.Context,
	db *gorm.DB,
	materialSetID uuid.UUID,
	seeds []SeedChunk,
	opt MaterialChunkExpandOptions,
) (map[uuid.UUID]float64, map[string]any, error) {
	trace := map[string]any{}
	if db == nil || materialSetID == uuid.Nil {
		return nil, nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	if opt.MaxSeeds <= 0 {
		opt.MaxSeeds = 12
	}
	if opt.MaxSeeds > 40 {
		opt.MaxSeeds = 40
	}
	if opt.MaxConcepts <= 0 {
		opt.MaxConcepts = 40
	}
	if opt.MaxConcepts > 120 {
		opt.MaxConcepts = 120
	}
	if opt.MaxEntities <= 0 {
		opt.MaxEntities = 30
	}
	if opt.MaxEntities > 120 {
		opt.MaxEntities = 120
	}
	if opt.MaxClaims <= 0 {
		opt.MaxClaims = 30
	}
	if opt.MaxClaims > 120 {
		opt.MaxClaims = 120
	}
	if opt.MaxEvidencePerConcept <= 0 {
		opt.MaxEvidencePerConcept = 10
	}
	if opt.MaxEvidencePerConcept > 40 {
		opt.MaxEvidencePerConcept = 40
	}
	if opt.MaxOut <= 0 {
		opt.MaxOut = 60
	}
	if opt.MaxOut > 160 {
		opt.MaxOut = 160
	}

	// Deduplicate + normalize seed scores to [0,1] (works across dense/lexical fallback scales).
	type seedNorm struct {
		ID    uuid.UUID
		Score float64
	}
	seedScore := map[uuid.UUID]float64{}
	maxScore := 0.0
	for _, s := range seeds {
		if s.ChunkID == uuid.Nil {
			continue
		}
		sc := s.Score
		if sc < 0 {
			sc = -sc
		}
		if sc > seedScore[s.ChunkID] {
			seedScore[s.ChunkID] = sc
		}
		if sc > maxScore {
			maxScore = sc
		}
	}
	seedList := make([]seedNorm, 0, len(seedScore))
	for id, sc := range seedScore {
		if id == uuid.Nil {
			continue
		}
		if maxScore > 0 {
			sc = sc / maxScore
		} else {
			sc = 1
		}
		seedList = append(seedList, seedNorm{ID: id, Score: sc})
	}
	sort.Slice(seedList, func(i, j int) bool { return seedList[i].Score > seedList[j].Score })
	if len(seedList) > opt.MaxSeeds {
		seedList = seedList[:opt.MaxSeeds]
	}
	trace["seed_count"] = len(seedList)
	if len(seedList) == 0 {
		return nil, trace, nil
	}

	allowIDs := mapKeysUUID(opt.AllowFileIDs)
	trace["allow_files"] = len(allowIDs)

	outScores := map[uuid.UUID]float64{}
	for _, s := range seedList {
		outScores[s.ID] += s.Score
	}

	seedIDs := make([]uuid.UUID, 0, len(seedList))
	for _, s := range seedList {
		seedIDs = append(seedIDs, s.ID)
	}

	// -----------------------------
	// Concept expansion (chunks -> concepts -> related concepts -> chunks)
	// -----------------------------
	type conceptEv struct {
		ConceptID uuid.UUID `gorm:"column:concept_id"`
		ChunkID   uuid.UUID `gorm:"column:material_chunk_id"`
		Weight    float64   `gorm:"column:weight"`
	}
	conceptScore := map[uuid.UUID]float64{}
	{
		start := time.Now()
		var rows []conceptEv
		err := db.WithContext(ctx).
			Model(&types.ConceptEvidence{}).
			Select("concept_id, material_chunk_id, weight").
			Where("material_chunk_id IN ?", seedIDs).
			Find(&rows).Error
		trace["concept_seed_ms"] = time.Since(start).Milliseconds()
		if err != nil {
			trace["concept_seed_err"] = err.Error()
		} else {
			trace["concept_seed_rows"] = len(rows)
			for _, r := range rows {
				if r.ConceptID == uuid.Nil || r.ChunkID == uuid.Nil {
					continue
				}
				seed := 0.0
				for _, s := range seedList {
					if s.ID == r.ChunkID {
						seed = s.Score
						break
					}
				}
				if seed <= 0 {
					continue
				}
				w := r.Weight
				if w <= 0 {
					w = 1
				}
				conceptScore[r.ConceptID] += seed * w
			}
		}
	}

	// Expand concepts using ConceptEdge in a single hop with conservative weights.
	type conceptEdge struct {
		FromID   uuid.UUID `gorm:"column:from_concept_id"`
		ToID     uuid.UUID `gorm:"column:to_concept_id"`
		EdgeType string    `gorm:"column:edge_type"`
		Strength float64   `gorm:"column:strength"`
	}
	{
		top := topKeysByScore(conceptScore, 30)
		if len(top) > 0 {
			base := map[uuid.UUID]float64{}
			for _, id := range top {
				base[id] = conceptScore[id]
			}
			start := time.Now()
			var edges []conceptEdge
			err := db.WithContext(ctx).
				Model(&types.ConceptEdge{}).
				Select("from_concept_id, to_concept_id, edge_type, strength").
				Where("from_concept_id IN ? OR to_concept_id IN ?", top, top).
				Find(&edges).Error
			trace["concept_edge_ms"] = time.Since(start).Milliseconds()
			if err != nil {
				trace["concept_edge_err"] = err.Error()
			} else {
				trace["concept_edge_rows"] = len(edges)
				delta := map[uuid.UUID]float64{}
				for _, e := range edges {
					if e.FromID == uuid.Nil || e.ToID == uuid.Nil {
						continue
					}
					s := e.Strength
					if s <= 0 {
						s = 0.5
					}
					switch strings.ToLower(strings.TrimSpace(e.EdgeType)) {
					case "prereq":
						// More useful to pull prerequisites for a concept than dependent/advanced neighbors.
						if base[e.ToID] > 0 {
							delta[e.FromID] += base[e.ToID] * s * 0.75
						}
						if base[e.FromID] > 0 {
							delta[e.ToID] += base[e.FromID] * s * 0.35
						}
					case "analogy":
						if base[e.FromID] > 0 {
							delta[e.ToID] += base[e.FromID] * s * 0.45
						}
						if base[e.ToID] > 0 {
							delta[e.FromID] += base[e.ToID] * s * 0.45
						}
					default: // related (or unknown)
						if base[e.FromID] > 0 {
							delta[e.ToID] += base[e.FromID] * s * 0.55
						}
						if base[e.ToID] > 0 {
							delta[e.FromID] += base[e.ToID] * s * 0.55
						}
					}
				}
				for id, v := range delta {
					conceptScore[id] += v
				}
			}
		}
	}

	// Pull evidence chunks for top concepts.
	{
		topConcepts := topKeysByScore(conceptScore, opt.MaxConcepts)
		trace["concept_top"] = len(topConcepts)
		if len(topConcepts) > 0 {
			start := time.Now()
			var rows []conceptEv
			q := db.WithContext(ctx).Raw(`
SELECT ce.concept_id, ce.material_chunk_id, ce.weight
FROM concept_evidence ce
JOIN material_chunk ch ON ce.material_chunk_id = ch.id
JOIN material_file mf ON ch.material_file_id = mf.id
WHERE mf.material_set_id = ? AND ce.concept_id IN ?
`, materialSetID, topConcepts)
			if len(allowIDs) > 0 {
				q = db.WithContext(ctx).Raw(`
SELECT ce.concept_id, ce.material_chunk_id, ce.weight
FROM concept_evidence ce
JOIN material_chunk ch ON ce.material_chunk_id = ch.id
JOIN material_file mf ON ch.material_file_id = mf.id
WHERE mf.material_set_id = ? AND ce.concept_id IN ? AND mf.id IN ?
`, materialSetID, topConcepts, allowIDs)
			}
			err := q.Scan(&rows).Error
			trace["concept_evidence_ms"] = time.Since(start).Milliseconds()
			if err != nil {
				trace["concept_evidence_err"] = err.Error()
			} else {
				trace["concept_evidence_rows"] = len(rows)

				// Cap evidence per concept for stability/perf.
				byConcept := map[uuid.UUID][]conceptEv{}
				for _, r := range rows {
					if r.ConceptID == uuid.Nil || r.ChunkID == uuid.Nil {
						continue
					}
					byConcept[r.ConceptID] = append(byConcept[r.ConceptID], r)
				}
				for cid, arr := range byConcept {
					sort.Slice(arr, func(i, j int) bool { return arr[i].Weight > arr[j].Weight })
					if len(arr) > opt.MaxEvidencePerConcept {
						arr = arr[:opt.MaxEvidencePerConcept]
					}
					byConcept[cid] = arr
				}

				for cid, arr := range byConcept {
					cs := conceptScore[cid]
					if cs <= 0 {
						continue
					}
					for _, r := range arr {
						w := r.Weight
						if w <= 0 {
							w = 1
						}
						outScores[r.ChunkID] += cs * w * 0.80
					}
				}
			}
		}
	}

	// -----------------------------
	// Entity expansion (chunks -> entities -> chunks)
	// -----------------------------
	type chunkEntity struct {
		ChunkID  uuid.UUID `gorm:"column:material_chunk_id"`
		EntityID uuid.UUID `gorm:"column:material_entity_id"`
		Weight   float64   `gorm:"column:weight"`
	}
	entityScore := map[uuid.UUID]float64{}
	{
		start := time.Now()
		var rows []chunkEntity
		err := db.WithContext(ctx).Raw(`
SELECT mce.material_chunk_id, mce.material_entity_id, mce.weight
FROM material_chunk_entity mce
JOIN material_entity me ON mce.material_entity_id = me.id
WHERE me.material_set_id = ? AND mce.material_chunk_id IN ?
`, materialSetID, seedIDs).Scan(&rows).Error
		trace["entity_seed_ms"] = time.Since(start).Milliseconds()
		if err != nil {
			trace["entity_seed_err"] = err.Error()
		} else {
			trace["entity_seed_rows"] = len(rows)
			seedNormByID := map[uuid.UUID]float64{}
			for _, s := range seedList {
				seedNormByID[s.ID] = s.Score
			}
			for _, r := range rows {
				if r.EntityID == uuid.Nil || r.ChunkID == uuid.Nil {
					continue
				}
				sc := seedNormByID[r.ChunkID]
				if sc <= 0 {
					continue
				}
				w := r.Weight
				if w <= 0 {
					w = 1
				}
				entityScore[r.EntityID] += sc * w * 0.70
			}
		}
	}
	{
		topEntities := topKeysByScore(entityScore, opt.MaxEntities)
		trace["entity_top"] = len(topEntities)
		if len(topEntities) > 0 {
			start := time.Now()
			var rows []chunkEntity
			q := db.WithContext(ctx).Raw(`
SELECT mce.material_chunk_id, mce.material_entity_id, mce.weight
FROM material_chunk_entity mce
JOIN material_chunk ch ON mce.material_chunk_id = ch.id
JOIN material_file mf ON ch.material_file_id = mf.id
WHERE mf.material_set_id = ? AND mce.material_entity_id IN ?
`, materialSetID, topEntities)
			if len(allowIDs) > 0 {
				q = db.WithContext(ctx).Raw(`
SELECT mce.material_chunk_id, mce.material_entity_id, mce.weight
FROM material_chunk_entity mce
JOIN material_chunk ch ON mce.material_chunk_id = ch.id
JOIN material_file mf ON ch.material_file_id = mf.id
WHERE mf.material_set_id = ? AND mce.material_entity_id IN ? AND mf.id IN ?
`, materialSetID, topEntities, allowIDs)
			}
			err := q.Scan(&rows).Error
			trace["entity_expand_ms"] = time.Since(start).Milliseconds()
			if err != nil {
				trace["entity_expand_err"] = err.Error()
			} else {
				trace["entity_expand_rows"] = len(rows)
				for _, r := range rows {
					es := entityScore[r.EntityID]
					if es <= 0 || r.ChunkID == uuid.Nil {
						continue
					}
					w := r.Weight
					if w <= 0 {
						w = 1
					}
					outScores[r.ChunkID] += es * w * 0.60
				}
			}
		}
	}

	// -----------------------------
	// Claim expansion (chunks -> claims -> chunks)
	// -----------------------------
	type chunkClaim struct {
		ChunkID uuid.UUID `gorm:"column:material_chunk_id"`
		ClaimID uuid.UUID `gorm:"column:material_claim_id"`
		Weight  float64   `gorm:"column:weight"`
	}
	claimScore := map[uuid.UUID]float64{}
	{
		start := time.Now()
		var rows []chunkClaim
		err := db.WithContext(ctx).Raw(`
SELECT mcc.material_chunk_id, mcc.material_claim_id, mcc.weight
FROM material_chunk_claim mcc
JOIN material_claim mc ON mcc.material_claim_id = mc.id
WHERE mc.material_set_id = ? AND mcc.material_chunk_id IN ?
`, materialSetID, seedIDs).Scan(&rows).Error
		trace["claim_seed_ms"] = time.Since(start).Milliseconds()
		if err != nil {
			trace["claim_seed_err"] = err.Error()
		} else {
			trace["claim_seed_rows"] = len(rows)
			seedNormByID := map[uuid.UUID]float64{}
			for _, s := range seedList {
				seedNormByID[s.ID] = s.Score
			}
			for _, r := range rows {
				if r.ClaimID == uuid.Nil || r.ChunkID == uuid.Nil {
					continue
				}
				sc := seedNormByID[r.ChunkID]
				if sc <= 0 {
					continue
				}
				w := r.Weight
				if w <= 0 {
					w = 1
				}
				claimScore[r.ClaimID] += sc * w * 0.80
			}
		}
	}
	{
		topClaims := topKeysByScore(claimScore, opt.MaxClaims)
		trace["claim_top"] = len(topClaims)
		if len(topClaims) > 0 {
			start := time.Now()
			var rows []chunkClaim
			q := db.WithContext(ctx).Raw(`
SELECT mcc.material_chunk_id, mcc.material_claim_id, mcc.weight
FROM material_chunk_claim mcc
JOIN material_chunk ch ON mcc.material_chunk_id = ch.id
JOIN material_file mf ON ch.material_file_id = mf.id
WHERE mf.material_set_id = ? AND mcc.material_claim_id IN ?
`, materialSetID, topClaims)
			if len(allowIDs) > 0 {
				q = db.WithContext(ctx).Raw(`
SELECT mcc.material_chunk_id, mcc.material_claim_id, mcc.weight
FROM material_chunk_claim mcc
JOIN material_chunk ch ON mcc.material_chunk_id = ch.id
JOIN material_file mf ON ch.material_file_id = mf.id
WHERE mf.material_set_id = ? AND mcc.material_claim_id IN ? AND mf.id IN ?
`, materialSetID, topClaims, allowIDs)
			}
			err := q.Scan(&rows).Error
			trace["claim_expand_ms"] = time.Since(start).Milliseconds()
			if err != nil {
				trace["claim_expand_err"] = err.Error()
			} else {
				trace["claim_expand_rows"] = len(rows)
				for _, r := range rows {
					cs := claimScore[r.ClaimID]
					if cs <= 0 || r.ChunkID == uuid.Nil {
						continue
					}
					w := r.Weight
					if w <= 0 {
						w = 1
					}
					outScores[r.ChunkID] += cs * w * 0.70
				}
			}
		}
	}

	// Final cap and normalization to keep scores roughly in [0, 1.5] (not required, but stable).
	type scored struct {
		ID    uuid.UUID
		Score float64
	}
	all := make([]scored, 0, len(outScores))
	best := 0.0
	for id, sc := range outScores {
		if id == uuid.Nil || sc <= 0 {
			continue
		}
		all = append(all, scored{ID: id, Score: sc})
		if sc > best {
			best = sc
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Score > all[j].Score })
	if len(all) > opt.MaxOut {
		all = all[:opt.MaxOut]
	}
	final := map[uuid.UUID]float64{}
	for _, s := range all {
		sc := s.Score
		if best > 0 {
			sc = sc / best
		}
		final[s.ID] = sc
	}
	trace["out"] = len(final)
	return final, trace, nil
}

func topKeysByScore(m map[uuid.UUID]float64, k int) []uuid.UUID {
	if len(m) == 0 || k <= 0 {
		return nil
	}
	type scored struct {
		ID    uuid.UUID
		Score float64
	}
	arr := make([]scored, 0, len(m))
	for id, sc := range m {
		if id == uuid.Nil || sc <= 0 {
			continue
		}
		arr = append(arr, scored{ID: id, Score: sc})
	}
	sort.Slice(arr, func(i, j int) bool { return arr[i].Score > arr[j].Score })
	if len(arr) > k {
		arr = arr[:k]
	}
	out := make([]uuid.UUID, 0, len(arr))
	for _, s := range arr {
		out = append(out, s.ID)
	}
	return out
}

func mapKeysUUID(in map[uuid.UUID]bool) []uuid.UUID {
	if len(in) == 0 {
		return nil
	}
	out := make([]uuid.UUID, 0, len(in))
	for id := range in {
		if id != uuid.Nil {
			out = append(out, id)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

