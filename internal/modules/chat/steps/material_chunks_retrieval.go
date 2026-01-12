package steps

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	chatrepo "github.com/yungbote/neurobridge-backend/internal/data/repos/chat"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/graphrag"
	learningIndex "github.com/yungbote/neurobridge-backend/internal/modules/learning/index"
	pc "github.com/yungbote/neurobridge-backend/internal/platform/pinecone"
)

type materialChunkHit struct {
	File  *types.MaterialFile
	Chunk *types.MaterialChunk
	Score float64
}

func retrieveMaterialChunkContext(
	ctx context.Context,
	deps ContextPlanDeps,
	userID uuid.UUID,
	pathID uuid.UUID,
	query string,
	qEmb []float32,
	tokenBudget int,
) (string, map[string]any) {
	trace := map[string]any{}
	if deps.DB == nil || userID == uuid.Nil || pathID == uuid.Nil || tokenBudget <= 0 {
		return "", nil
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return "", nil
	}

	// Resolve material_set_id for this path (authoritative linkage).
	var idx types.UserLibraryIndex
	_ = deps.DB.WithContext(ctx).
		Model(&types.UserLibraryIndex{}).
		Where("user_id = ? AND path_id = ?", userID, pathID).
		Limit(1).
		Find(&idx).Error
	if idx.ID == uuid.Nil || idx.MaterialSetID == uuid.Nil {
		return "", nil
	}
	trace["material_set_hash"] = shortHash(idx.MaterialSetID.String())

	mode := ""
	candidates := make([]materialChunkHit, 0, 24)

	// 1) Dense retrieval via Pinecone chunk vectors.
	if deps.Vec != nil && len(qEmb) > 0 {
		cands, m := denseChunkCandidates(ctx, deps.DB, deps.Vec, idx.MaterialSetID, qEmb, 28)
		if len(m) > 0 {
			trace["dense"] = m
		}
		if len(cands) > 0 {
			mode = "dense_pinecone"
			candidates = cands
		}
	}

	// 2) Dense SQL fallback (cosine over stored embeddings).
	if len(candidates) == 0 && len(qEmb) > 0 {
		cands, m := denseChunkCandidatesSQL(ctx, deps.DB, idx.MaterialSetID, qEmb, 28)
		if len(m) > 0 {
			trace["dense_sql"] = m
		}
		if len(cands) > 0 {
			mode = "dense_sql"
			candidates = cands
		}
	}

	// 3) Lexical fallback (Postgres FTS on chunk text).
	if len(candidates) == 0 {
		cands, m := lexicalChunkCandidatesSQL(ctx, deps.DB, idx.MaterialSetID, query, 18)
		if len(m) > 0 {
			trace["lexical_sql"] = m
		}
		if len(cands) > 0 {
			mode = "lexical_sql"
			candidates = cands
		}
	}

	trace["mode"] = mode
	trace["candidates"] = len(candidates)

	if len(candidates) == 0 {
		return "", trace
	}

	// Filter low-signal and prompt-injection-ish chunks.
	allowPrompty := queryMentionsPrompt(query)
	filtered := make([]materialChunkHit, 0, len(candidates))
	for _, h := range candidates {
		if h.Chunk == nil || h.File == nil {
			continue
		}
		text := strings.TrimSpace(h.Chunk.Text)
		if text == "" || isLowSignal(text) {
			continue
		}
		if looksLikePromptInjection(text) && !allowPrompty {
			continue
		}
		filtered = append(filtered, h)
	}
	candidates = filtered
	trace["kept"] = len(candidates)
	if len(candidates) == 0 {
		return "", trace
	}

	// Graph-assisted expansion (best-effort): expand via ConceptEvidence/ConceptEdge and material entities/claims.
	// This improves multi-hop retrieval while keeping vector retrieval as the base (seeds are always retained).
	{
		seeds := make([]graphrag.SeedChunk, 0, len(candidates))
		for _, h := range candidates {
			if h.Chunk == nil || h.Chunk.ID == uuid.Nil {
				continue
			}
			seeds = append(seeds, graphrag.SeedChunk{ChunkID: h.Chunk.ID, Score: h.Score})
			if len(seeds) >= 12 {
				break
			}
		}
		if len(seeds) > 0 {
			scores, gtrace, err := graphrag.ExpandMaterialChunkScores(ctx, deps.DB, idx.MaterialSetID, seeds, graphrag.MaterialChunkExpandOptions{
				MaxSeeds:              12,
				MaxConcepts:           45,
				MaxEntities:           30,
				MaxClaims:             30,
				MaxEvidencePerConcept: 10,
				MaxOut:                70,
			})
			if err != nil {
				trace["graph_expand_err"] = err.Error()
			}
			if len(gtrace) > 0 {
				trace["graph_expand"] = gtrace
			}
			if len(scores) > 0 {
				type scoredID struct {
					ID    uuid.UUID
					Score float64
				}
				sorted := make([]scoredID, 0, len(scores))
				for id, sc := range scores {
					if id == uuid.Nil || sc <= 0 {
						continue
					}
					sorted = append(sorted, scoredID{ID: id, Score: sc})
				}
				sort.Slice(sorted, func(i, j int) bool { return sorted[i].Score > sorted[j].Score })

				ids := make([]uuid.UUID, 0, len(sorted))
				scoreByID := map[uuid.UUID]float64{}
				for _, s := range sorted {
					ids = append(ids, s.ID)
					scoreByID[s.ID] = s.Score
				}

				expanded := loadMaterialHitsByChunkIDs(ctx, deps.DB, idx.MaterialSetID, ids, scoreByID, mode+"+graph")
				if len(expanded) > 0 {
					mode = mode + "+graph"
					candidates = expanded
				}
			}
		}
	}

	// Re-apply hard filters after graph expansion (newly-added chunks are also untrusted).
	{
		filtered := make([]materialChunkHit, 0, len(candidates))
		for _, h := range candidates {
			if h.Chunk == nil || h.File == nil {
				continue
			}
			text := strings.TrimSpace(h.Chunk.Text)
			if text == "" || isLowSignal(text) {
				continue
			}
			if looksLikePromptInjection(text) && !allowPrompty {
				continue
			}
			filtered = append(filtered, h)
		}
		candidates = filtered
		trace["kept_after_graph"] = len(candidates)
		if len(candidates) == 0 {
			return "", trace
		}
	}

	// Sort best-first by relevance.
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Score > candidates[j].Score })

	selected := selectMaterialHitsForQuery(query, candidates, tokenBudget)
	trace["selected"] = len(selected)
	if len(selected) == 0 {
		return "", trace
	}

	// Include stable identifiers for explainability/provenance graphs.
	selectedChunkIDs := make([]string, 0, len(selected))
	selectedChunks := make([]any, 0, len(selected))
	for _, h := range selected {
		if h.Chunk == nil || h.Chunk.ID == uuid.Nil || h.File == nil || h.File.ID == uuid.Nil {
			continue
		}
		selectedChunkIDs = append(selectedChunkIDs, h.Chunk.ID.String())
		selectedChunks = append(selectedChunks, map[string]any{
			"chunk_id": h.Chunk.ID.String(),
			"file_id":  h.File.ID.String(),
			"score":    h.Score,
		})
	}
	trace["selected_chunk_ids"] = selectedChunkIDs
	trace["selected_chunks"] = selectedChunks

	return renderMaterialHitContext(selected), trace
}

func denseChunkCandidates(ctx context.Context, db *gorm.DB, vec pc.VectorStore, materialSetID uuid.UUID, qEmb []float32, limit int) ([]materialChunkHit, map[string]any) {
	trace := map[string]any{}
	if db == nil || vec == nil || materialSetID == uuid.Nil || len(qEmb) == 0 || limit <= 0 {
		return nil, nil
	}

	ns := learningIndex.ChunksNamespace(materialSetID)
	filter := map[string]any{"type": "chunk"}

	start := time.Now()
	qctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	matches, err := vec.QueryMatches(qctx, ns, qEmb, limit, filter)
	cancel()
	trace["ms"] = time.Since(start).Milliseconds()
	if err != nil {
		trace["err"] = err.Error()
		return nil, trace
	}
	trace["count"] = len(matches)
	if len(matches) == 0 {
		return nil, trace
	}

	scoreByID := map[uuid.UUID]float64{}
	chunkIDs := make([]uuid.UUID, 0, len(matches))
	for _, m := range matches {
		id, err := uuid.Parse(strings.TrimSpace(m.ID))
		if err != nil || id == uuid.Nil {
			continue
		}
		chunkIDs = append(chunkIDs, id)
		scoreByID[id] = m.Score
	}
	if len(chunkIDs) == 0 {
		return nil, trace
	}

	return loadMaterialHitsByChunkIDs(ctx, db, materialSetID, chunkIDs, scoreByID, "dense_pinecone"), trace
}

func denseChunkCandidatesSQL(ctx context.Context, db *gorm.DB, materialSetID uuid.UUID, qEmb []float32, limit int) ([]materialChunkHit, map[string]any) {
	trace := map[string]any{}
	if db == nil || materialSetID == uuid.Nil || len(qEmb) == 0 || limit <= 0 {
		return nil, nil
	}

	start := time.Now()
	candidateLimit := 1200
	var rows []*types.MaterialChunk
	_ = db.WithContext(ctx).
		Model(&types.MaterialChunk{}).
		Joins("JOIN material_file ON material_chunk.material_file_id = material_file.id").
		Where("material_file.material_set_id = ?", materialSetID).
		Where("material_chunk.embedding <> '[]'::jsonb").
		Order("material_chunk.created_at DESC").
		Limit(candidateLimit).
		Find(&rows).Error
	trace["ms"] = time.Since(start).Milliseconds()
	trace["scanned"] = len(rows)
	if len(rows) == 0 {
		return nil, trace
	}

	type scored struct {
		ch    *types.MaterialChunk
		score float64
	}
	scoredRows := make([]scored, 0, len(rows))
	for _, ch := range rows {
		if ch == nil || ch.ID == uuid.Nil {
			continue
		}
		emb, _ := chatrepo.ParseEmbeddingJSON(ch.Embedding)
		if len(emb) == 0 || len(emb) != len(qEmb) {
			continue
		}
		scoredRows = append(scoredRows, scored{ch: ch, score: cosine(qEmb, emb)})
	}
	if len(scoredRows) == 0 {
		return nil, trace
	}
	sort.Slice(scoredRows, func(i, j int) bool { return scoredRows[i].score > scoredRows[j].score })
	if len(scoredRows) > limit {
		scoredRows = scoredRows[:limit]
	}

	scoreByID := map[uuid.UUID]float64{}
	chunkIDs := make([]uuid.UUID, 0, len(scoredRows))
	for _, s := range scoredRows {
		if s.ch == nil || s.ch.ID == uuid.Nil {
			continue
		}
		chunkIDs = append(chunkIDs, s.ch.ID)
		scoreByID[s.ch.ID] = s.score
	}
	if len(chunkIDs) == 0 {
		return nil, trace
	}

	return loadMaterialHitsByChunkIDs(ctx, db, materialSetID, chunkIDs, scoreByID, "dense_sql"), trace
}

func lexicalChunkCandidatesSQL(ctx context.Context, db *gorm.DB, materialSetID uuid.UUID, query string, limit int) ([]materialChunkHit, map[string]any) {
	trace := map[string]any{}
	if db == nil || materialSetID == uuid.Nil || strings.TrimSpace(query) == "" || limit <= 0 {
		return nil, nil
	}

	start := time.Now()
	sql := fmt.Sprintf(`
		SELECT material_chunk.*,
		       ts_rank(to_tsvector('english', material_chunk.text), plainto_tsquery('english', ?)) AS rank
		FROM material_chunk
		JOIN material_file ON material_chunk.material_file_id = material_file.id
		WHERE material_file.material_set_id = ?
			AND to_tsvector('english', material_chunk.text) @@ plainto_tsquery('english', ?)
		ORDER BY rank DESC, material_chunk.created_at DESC
		LIMIT %d;
	`, limit)

	type row struct {
		types.MaterialChunk
		Rank float64 `gorm:"column:rank"`
	}
	var rows []row
	_ = db.WithContext(ctx).Raw(sql, query, materialSetID, query).Scan(&rows).Error
	trace["ms"] = time.Since(start).Milliseconds()
	trace["count"] = len(rows)
	if len(rows) == 0 {
		return nil, trace
	}

	scoreByID := map[uuid.UUID]float64{}
	chunkIDs := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		if r.ID == uuid.Nil {
			continue
		}
		chunkIDs = append(chunkIDs, r.ID)
		scoreByID[r.ID] = r.Rank
	}
	if len(chunkIDs) == 0 {
		return nil, trace
	}

	return loadMaterialHitsByChunkIDs(ctx, db, materialSetID, chunkIDs, scoreByID, "lexical_sql"), trace
}

func loadMaterialHitsByChunkIDs(
	ctx context.Context,
	db *gorm.DB,
	materialSetID uuid.UUID,
	chunkIDs []uuid.UUID,
	scoreByID map[uuid.UUID]float64,
	mode string,
) []materialChunkHit {
	if db == nil || materialSetID == uuid.Nil || len(chunkIDs) == 0 {
		return nil
	}

	// Load chunks and verify they belong to the set.
	var chunks []*types.MaterialChunk
	_ = db.WithContext(ctx).
		Model(&types.MaterialChunk{}).
		Joins("JOIN material_file ON material_chunk.material_file_id = material_file.id").
		Where("material_file.material_set_id = ?", materialSetID).
		Where("material_chunk.id IN ?", chunkIDs).
		Find(&chunks).Error

	chunkByID := map[uuid.UUID]*types.MaterialChunk{}
	fileIDs := make([]uuid.UUID, 0, len(chunks))
	seenFiles := map[uuid.UUID]bool{}
	for _, ch := range chunks {
		if ch == nil || ch.ID == uuid.Nil || ch.MaterialFileID == uuid.Nil {
			continue
		}
		chunkByID[ch.ID] = ch
		if !seenFiles[ch.MaterialFileID] {
			seenFiles[ch.MaterialFileID] = true
			fileIDs = append(fileIDs, ch.MaterialFileID)
		}
	}
	if len(chunkByID) == 0 || len(fileIDs) == 0 {
		return nil
	}

	// Load file metadata.
	var files []*types.MaterialFile
	_ = db.WithContext(ctx).
		Model(&types.MaterialFile{}).
		Where("id IN ?", fileIDs).
		Find(&files).Error
	fileByID := map[uuid.UUID]*types.MaterialFile{}
	for _, f := range files {
		if f != nil && f.ID != uuid.Nil {
			fileByID[f.ID] = f
		}
	}

	// Preserve Pinecone ordering when available, otherwise sort by score later.
	hits := make([]materialChunkHit, 0, len(chunkIDs))
	for _, id := range chunkIDs {
		ch := chunkByID[id]
		if ch == nil || ch.MaterialFileID == uuid.Nil {
			continue
		}
		f := fileByID[ch.MaterialFileID]
		if f == nil {
			continue
		}
		hits = append(hits, materialChunkHit{
			File:  f,
			Chunk: ch,
			Score: scoreByID[id],
		})
	}

	_ = mode // for future debugging hooks; keep signature stable
	return hits
}

func selectMaterialHitsForQuery(query string, hits []materialChunkHit, tokenBudget int) []materialChunkHit {
	if len(hits) == 0 {
		return nil
	}
	query = strings.ToLower(strings.TrimSpace(query))

	maxTotal := 8
	if tokenBudget >= 3000 {
		maxTotal = 10
	} else if tokenBudget <= 1400 {
		maxTotal = 6
	}

	mentionsFiles := strings.Contains(query, "file") ||
		strings.Contains(query, "files") ||
		strings.Contains(query, "source") ||
		strings.Contains(query, "sources") ||
		strings.Contains(query, "material") ||
		strings.Contains(query, "materials") ||
		strings.Contains(query, "upload") ||
		strings.Contains(query, "document")
	mentionsAll := strings.Contains(query, "all") || strings.Contains(query, "each") || strings.Contains(query, "every")

	// If the user is asking about "all/each file", diversify to cover multiple files first.
	if mentionsFiles && mentionsAll {
		bestByFile := map[uuid.UUID]materialChunkHit{}
		for _, h := range hits {
			if h.File == nil || h.Chunk == nil || h.File.ID == uuid.Nil {
				continue
			}
			prev, ok := bestByFile[h.File.ID]
			if !ok || h.Score > prev.Score {
				bestByFile[h.File.ID] = h
			}
		}
		best := make([]materialChunkHit, 0, len(bestByFile))
		for _, h := range bestByFile {
			best = append(best, h)
		}
		sort.Slice(best, func(i, j int) bool { return best[i].Score > best[j].Score })
		if len(best) > maxTotal {
			best = best[:maxTotal]
		}
		return best
	}

	// Default: allow a couple chunks per file, avoid dominance by one doc.
	perFile := map[uuid.UUID]int{}
	out := make([]materialChunkHit, 0, maxTotal)
	maxPerFile := 2
	for _, h := range hits {
		if h.File == nil || h.File.ID == uuid.Nil || h.Chunk == nil {
			continue
		}
		if perFile[h.File.ID] >= maxPerFile {
			continue
		}
		out = append(out, h)
		perFile[h.File.ID]++
		if len(out) >= maxTotal {
			break
		}
	}
	return out
}

func renderMaterialHitContext(hits []materialChunkHit) string {
	var b strings.Builder
	for _, h := range hits {
		if h.File == nil || h.Chunk == nil {
			continue
		}
		name := strings.TrimSpace(h.File.OriginalName)
		if name == "" {
			name = "Untitled file"
		}
		t := materialFileTypeLabel(h.File)
		header := name
		if t != "" {
			header += " (" + t + ")"
		}
		if h.Chunk.ID != uuid.Nil {
			header = "[chunk_id=" + h.Chunk.ID.String() + "] " + header
		}
		if loc := chunkLocation(h.Chunk); loc != "" {
			header += " — " + loc
		}
		b.WriteString("- " + header + "\n")
		b.WriteString("  " + trimToChars(strings.TrimSpace(h.Chunk.Text), 900) + "\n\n")
	}
	return strings.TrimSpace(b.String())
}

func chunkLocation(ch *types.MaterialChunk) string {
	if ch == nil {
		return ""
	}
	if ch.Page != nil && *ch.Page > 0 {
		return fmt.Sprintf("page %d", *ch.Page)
	}
	if ch.StartSec != nil && ch.EndSec != nil && *ch.StartSec >= 0 && *ch.EndSec >= 0 {
		return fmt.Sprintf("%s–%s", formatHMS(*ch.StartSec), formatHMS(*ch.EndSec))
	}
	if ch.StartSec != nil && *ch.StartSec >= 0 {
		return formatHMS(*ch.StartSec)
	}
	return ""
}

func formatHMS(sec float64) string {
	if sec < 0 {
		sec = 0
	}
	s := int(sec + 0.5)
	h := s / 3600
	m := (s % 3600) / 60
	ss := s % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, ss)
	}
	return fmt.Sprintf("%d:%02d", m, ss)
}
