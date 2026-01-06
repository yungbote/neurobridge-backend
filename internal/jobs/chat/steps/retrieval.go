package steps

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	chatIndex "github.com/yungbote/neurobridge-backend/internal/chat/index"
	pc "github.com/yungbote/neurobridge-backend/internal/clients/pinecone"
	chatrepo "github.com/yungbote/neurobridge-backend/internal/data/repos/chat"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
)

type HybridRetrieveOutput struct {
	Docs  []*types.ChatDoc
	Mode  string
	Trace map[string]any

	// QueryEmbedding is the embedding used for dense retrieval.
	// It is not persisted; callers may reuse it for other retrieval passes (e.g., source materials).
	QueryEmbedding []float32 `json:"-"`
}

type retrievalCandidate struct {
	Doc *types.ChatDoc

	DenseScore  float64
	DenseHit    bool
	LexicalRank float64
	LexicalHit  bool

	RerankScore float64

	InjectionFlag bool
	DropReason    string
}

func hybridRetrieve(ctx context.Context, deps ContextPlanDeps, thread *types.ChatThread, query string) (HybridRetrieveOutput, error) {
	out := HybridRetrieveOutput{Mode: "normal", Trace: map[string]any{}}
	if deps.AI == nil || deps.Docs == nil {
		return out, fmt.Errorf("hybridRetrieve: missing deps")
	}
	if thread == nil || thread.ID == uuid.Nil || thread.UserID == uuid.Nil {
		return out, fmt.Errorf("hybridRetrieve: missing thread/user")
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return out, nil
	}

	embedStart := time.Now()
	embCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	embs, err := deps.AI.Embed(embCtx, []string{query})
	if err != nil {
		out.Mode = "degraded_embed"
		out.Trace["embed_err"] = err.Error()
		// Still allow lexical-only retrieval.
	}
	var qEmb []float32
	if len(embs) > 0 {
		qEmb = embs[0]
	}
	out.QueryEmbedding = qEmb
	out.Trace["embed_ms"] = time.Since(embedStart).Milliseconds()

	docTypes := []string{
		DocTypeMessageChunk,
		DocTypeSummary,
		DocTypeMemory,
		DocTypeEntity,
		DocTypeClaim,
		DocTypePathOverview,
		DocTypePathNode,
		DocTypePathConcepts,
		DocTypePathMaterials,
		DocTypePathUnitDoc,
	}
	maxCandidatesPerScope := 40

	candidates := map[uuid.UUID]*retrievalCandidate{}
	degradedDense := false
	degradedLex := false

	addCandidate := func(c *retrievalCandidate) {
		if c == nil || c.Doc == nil {
			return
		}
		ex, ok := candidates[c.Doc.ID]
		if !ok || ex == nil {
			candidates[c.Doc.ID] = c
			return
		}
		if c.DenseHit {
			ex.DenseHit = true
			if c.DenseScore > ex.DenseScore {
				ex.DenseScore = c.DenseScore
			}
		}
		if c.LexicalHit {
			ex.LexicalHit = true
			if c.LexicalRank > ex.LexicalRank {
				ex.LexicalRank = c.LexicalRank
			}
		}
	}

	addScope := func(scope string, scopeID *uuid.UUID) error {
		scopeTrace := map[string]any{
			"scope": scope,
		}
		if scopeID != nil && *scopeID != uuid.Nil {
			scopeTrace["scope_id"] = scopeID.String()
		}

		// Dense: Pinecone first, SQL fallback if Pinecone is unavailable/degraded.
		denseStart := time.Now()
		if deps.Vec != nil && len(qEmb) > 0 {
			filter := map[string]any{
				"user_id": thread.UserID.String(),
				"scope":   scope,
			}
			if scopeID != nil && *scopeID != uuid.Nil {
				filter["scope_id"] = scopeID.String()
			}

			denseCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			matches, qErr := deps.Vec.QueryMatches(denseCtx, chatIndex.ChatUserNamespace(thread.UserID), qEmb, maxCandidatesPerScope, filter)
			cancel()
			scopeTrace["dense_ms"] = time.Since(denseStart).Milliseconds()
			if qErr != nil {
				degradedDense = true
				scopeTrace["dense_err"] = qErr.Error()
			} else {
				scopeTrace["dense_count"] = len(matches)
				if len(matches) > 0 {
					scopeTrace["dense_top_score"] = matches[0].Score
				}
				docIDs := make([]uuid.UUID, 0, len(matches))
				scoreByID := map[uuid.UUID]float64{}
				for _, m := range matches {
					uid, err := uuid.Parse(strings.TrimSpace(m.ID))
					if err != nil || uid == uuid.Nil {
						continue
					}
					docIDs = append(docIDs, uid)
					scoreByID[uid] = m.Score
				}
				if len(docIDs) > 0 {
					rows, err := deps.Docs.GetByIDs(dbctx.Context{Ctx: ctx, Tx: deps.DB}, thread.UserID, docIDs)
					if err != nil {
						return err
					}
					for _, d := range rows {
						if d == nil {
							continue
						}
						// Defense-in-depth: verify scope constraints even if Pinecone filter misbehaves.
						if strings.TrimSpace(d.Scope) != scope {
							continue
						}
						if scopeID != nil && *scopeID != uuid.Nil {
							if d.ScopeID == nil || *d.ScopeID != *scopeID {
								continue
							}
						} else if d.ScopeID != nil && *d.ScopeID != uuid.Nil {
							continue
						}
						addCandidate(&retrievalCandidate{
							Doc:        d,
							DenseHit:   true,
							DenseScore: scoreByID[d.ID],
						})
					}
				}
			}
		}

		// SQL dense fallback if Pinecone degraded/unavailable and we have embeddings.
		if (deps.Vec == nil || degradedDense) && len(qEmb) > 0 && deps.DB != nil {
			sqlStart := time.Now()
			limit := 1200
			var rows []*types.ChatDoc
			q := deps.DB.WithContext(ctx).
				Model(&types.ChatDoc{}).
				Where("user_id = ? AND scope = ?", thread.UserID, scope)
			if scopeID != nil && *scopeID != uuid.Nil {
				q = q.Where("scope_id = ?", *scopeID)
			} else {
				q = q.Where("scope_id IS NULL")
			}
			if len(docTypes) > 0 {
				q = q.Where("doc_type IN ?", docTypes)
			}
			q = q.Where("embedding <> '[]'::jsonb").Order("created_at DESC").Limit(limit)
			_ = q.Find(&rows).Error
			type scored struct {
				d     *types.ChatDoc
				score float64
			}
			scoredRows := make([]scored, 0, len(rows))
			for _, d := range rows {
				if d == nil {
					continue
				}
				emb, _ := chatrepo.ParseEmbeddingJSON(d.Embedding)
				if len(emb) == 0 || len(emb) != len(qEmb) {
					continue
				}
				scoredRows = append(scoredRows, scored{d: d, score: cosine(qEmb, emb)})
			}
			sort.Slice(scoredRows, func(i, j int) bool { return scoredRows[i].score > scoredRows[j].score })
			if len(scoredRows) > maxCandidatesPerScope {
				scoredRows = scoredRows[:maxCandidatesPerScope]
			}
			scopeTrace["dense_sql_ms"] = time.Since(sqlStart).Milliseconds()
			scopeTrace["dense_sql_count"] = len(scoredRows)
			if len(scoredRows) > 0 {
				scopeTrace["dense_sql_top_score"] = scoredRows[0].score
			}
			for _, s := range scoredRows {
				addCandidate(&retrievalCandidate{Doc: s.d, DenseHit: true, DenseScore: s.score})
			}
		}

		// Lexical (Postgres FTS over contextual_text).
		lexStart := time.Now()
		lexCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		hits, lerr := deps.Docs.LexicalSearchHits(dbctx.Context{Ctx: lexCtx, Tx: deps.DB}, chatrepo.ChatLexicalQuery{
			UserID:   thread.UserID,
			Scope:    scope,
			ScopeID:  scopeID,
			DocTypes: docTypes,
			Query:    query,
			Limit:    maxCandidatesPerScope,
		})
		cancel()
		scopeTrace["lex_ms"] = time.Since(lexStart).Milliseconds()
		if lerr != nil {
			degradedLex = true
			scopeTrace["lex_err"] = lerr.Error()
		} else {
			scopeTrace["lex_count"] = len(hits)
			if len(hits) > 0 {
				scopeTrace["lex_top_rank"] = hits[0].Rank
			}
			for _, h := range hits {
				if h.Doc == nil {
					continue
				}
				addCandidate(&retrievalCandidate{Doc: h.Doc, LexicalHit: true, LexicalRank: h.Rank})
			}
		}

		if out.Trace["scopes"] == nil {
			out.Trace["scopes"] = []any{}
		}
		out.Trace["scopes"] = append(out.Trace["scopes"].([]any), scopeTrace)
		return nil
	}

	// Expansion order: thread -> path -> user.
	if err := addScope(ScopeThread, &thread.ID); err != nil {
		return out, err
	}
	if thread.PathID != nil && *thread.PathID != uuid.Nil {
		if err := addScope(ScopePath, thread.PathID); err != nil {
			return out, err
		}
	}
	if len(candidates) < 30 {
		if err := addScope(ScopeUser, nil); err != nil {
			return out, err
		}
	}

	// Collapse to a stable list.
	all := make([]*retrievalCandidate, 0, len(candidates))
	for _, c := range candidates {
		all = append(all, c)
	}
	sort.Slice(all, func(i, j int) bool {
		ai, aj := time.Time{}, time.Time{}
		if all[i] != nil && all[i].Doc != nil {
			ai = all[i].Doc.CreatedAt
		}
		if all[j] != nil && all[j].Doc != nil {
			aj = all[j].Doc.CreatedAt
		}
		return ai.After(aj)
	})
	if len(all) > 60 {
		all = all[:60]
	}

	// Hard gates: prompt-injection-ish content and low-signal garbage.
	droppedInjection := 0
	allowPrompty := queryMentionsPrompt(query)
	filtered := make([]*retrievalCandidate, 0, len(all))
	for _, c := range all {
		if c == nil || c.Doc == nil {
			continue
		}
		text := strings.TrimSpace(c.Doc.ContextualText)
		if text == "" {
			text = strings.TrimSpace(c.Doc.Text)
		}
		if isLowSignal(text) {
			c.DropReason = "low_signal"
			continue
		}
		if looksLikePromptInjection(text) && !allowPrompty {
			c.InjectionFlag = true
			c.DropReason = "prompt_injection"
			droppedInjection++
			continue
		}
		filtered = append(filtered, c)
	}
	all = filtered
	out.Trace["dropped_injection"] = droppedInjection

	if degradedDense {
		out.Trace["degraded_dense"] = true
	}
	if degradedLex {
		out.Trace["degraded_lexical"] = true
	}

	if len(all) == 0 {
		if droppedInjection > 0 {
			out.Mode = "empty_poisoned"
		} else if degradedDense || degradedLex {
			out.Mode = "empty_degraded"
		} else {
			out.Mode = "empty"
		}
		return out, nil
	}

	// Rerank with LLM.
	rerankStart := time.Now()
	var items strings.Builder
	for _, c := range all {
		d := c.Doc
		items.WriteString("- id=" + d.ID.String() + " type=" + strings.TrimSpace(d.DocType) + " scope=" + strings.TrimSpace(d.Scope))
		if d.SourceSeq != nil {
			items.WriteString(" source_seq=" + itoa64(*d.SourceSeq))
		}
		items.WriteString("\n" + trimToChars(d.ContextualText, 700) + "\n\n")
	}

	sys, usr := promptRerank(query, items.String())
	rerankCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	obj, rerr := deps.AI.GenerateJSON(rerankCtx, sys, usr, "chat_rerank", schemaRerank())
	cancel()
	out.Trace["rerank_ms"] = time.Since(rerankStart).Milliseconds()
	if rerr != nil {
		out.Trace["rerank_err"] = rerr.Error()
	}

	scoreMap := map[string]float64{}
	if rerr == nil {
		if rs, ok := obj["results"].([]any); ok {
			for _, r := range rs {
				m, _ := r.(map[string]any)
				id := strings.TrimSpace(asString(m["id"]))
				score := asFloat(m["score"])
				if id != "" {
					scoreMap[id] = score
				}
			}
		}
	}

	// Build scored docs with scope priors and confidence gating.
	scored := make([]scoredDoc, 0, len(all))
	var (
		bestScore       float64
		bestThreadScore float64
	)
	for _, c := range all {
		if c == nil || c.Doc == nil {
			continue
		}
		base := scoreMap[c.Doc.ID.String()]
		if rerr != nil {
			// Fallback: combine lexical + dense in a rough way when rerank fails.
			base = 0
			if c.DenseHit {
				base += 35
			}
			if c.LexicalHit {
				base += 35
			}
			base += math.Min(30, math.Abs(c.LexicalRank*100))
		}
		// Scope priors (thread > path > user).
		switch strings.TrimSpace(c.Doc.Scope) {
		case ScopeThread:
			base += 4
		case ScopePath:
			base += 2
		}
		c.RerankScore = base

		if base > bestScore {
			bestScore = base
		}
		if strings.TrimSpace(c.Doc.Scope) == ScopeThread && base > bestThreadScore {
			bestThreadScore = base
		}

		emb, _ := chatrepo.ParseEmbeddingJSON(c.Doc.Embedding)
		scored = append(scored, scoredDoc{Doc: c.Doc, Score: base, Emb: emb})
	}

	out.Trace["rerank_top_score"] = bestScore

	// Confidence gate: if nothing clears a reasonable threshold, treat retrieval as empty.
	minKeep := 55.0
	if rerr != nil {
		minKeep = 50.0
	}
	filteredScored := make([]scoredDoc, 0, len(scored))
	for _, s := range scored {
		if s.Doc == nil {
			continue
		}
		if s.Score < minKeep {
			continue
		}
		// Scope sanity: if thread has a strong hit, only keep non-thread hits that compete.
		if bestThreadScore >= 70 && strings.TrimSpace(s.Doc.Scope) != ScopeThread {
			if s.Score < bestThreadScore-10 {
				continue
			}
		}
		filteredScored = append(filteredScored, s)
	}
	scored = filteredScored
	out.Trace["kept_after_threshold"] = len(scored)

	if len(scored) == 0 {
		out.Mode = "empty_weak"
		return out, nil
	}

	selected := mmrSelect(scored, 18, 0.65)
	docsOut := make([]*types.ChatDoc, 0, len(selected))
	for _, s := range selected {
		docsOut = append(docsOut, s.Doc)
	}
	out.Docs = docsOut
	out.Trace["selected"] = selectedDocTrace(docsOut)
	if degradedDense && !degradedLex {
		out.Mode = "degraded_dense"
	} else if degradedLex && !degradedDense {
		out.Mode = "degraded_lexical"
	} else if degradedDense && degradedLex {
		out.Mode = "degraded_both"
	}
	return out, nil
}

func selectedDocTrace(docs []*types.ChatDoc) []any {
	out := make([]any, 0, len(docs))
	for _, d := range docs {
		if d == nil {
			continue
		}
		row := map[string]any{
			"doc_id":   d.ID.String(),
			"doc_type": strings.TrimSpace(d.DocType),
			"scope":    strings.TrimSpace(d.Scope),
		}
		if d.SourceID != nil && *d.SourceID != uuid.Nil {
			row["source_id"] = d.SourceID.String()
		}
		if d.SourceSeq != nil {
			row["source_seq"] = *d.SourceSeq
		}
		out = append(out, row)
	}
	return out
}

func isLowSignal(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) < 24 {
		return true
	}
	alnum := 0
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			alnum++
		}
	}
	// Very rough: if it's mostly punctuation/whitespace, it's not helpful retrieval evidence.
	return float64(alnum) < float64(len([]rune(s)))*0.25
}

func queryMentionsPrompt(q string) bool {
	q = strings.ToLower(q)
	return strings.Contains(q, "system prompt") ||
		strings.Contains(q, "developer message") ||
		strings.Contains(q, "prompt injection") ||
		strings.Contains(q, "jailbreak") ||
		strings.Contains(q, "ignore previous") ||
		strings.Contains(q, "instructions")
}

func looksLikePromptInjection(s string) bool {
	s = strings.ToLower(s)
	patterns := []string{
		"ignore previous",
		"system prompt",
		"developer message",
		"you are chatgpt",
		"act as",
		"follow these instructions",
		"jailbreak",
		"do not follow",
		"override",
		"BEGIN SYSTEM",
		"END SYSTEM",
	}
	for _, p := range patterns {
		if strings.Contains(s, strings.ToLower(p)) {
			return true
		}
	}
	return false
}

func graphContext(dbc dbctx.Context, userID uuid.UUID, retrieved []*types.ChatDoc, tokenBudget int) (string, error) {
	db := dbc.Tx
	if db == nil || userID == uuid.Nil || len(retrieved) == 0 {
		return "", nil
	}
	// Extract entity IDs from retrieved docs.
	entityIDs := make([]uuid.UUID, 0, 16)
	for _, d := range retrieved {
		if d == nil {
			continue
		}
		if strings.TrimSpace(d.DocType) == DocTypeEntity && d.SourceID != nil && *d.SourceID != uuid.Nil {
			entityIDs = append(entityIDs, *d.SourceID)
		}
		if len(entityIDs) >= 10 {
			break
		}
	}
	if len(entityIDs) == 0 {
		// Still include claims if present.
		lines := ""
		for _, d := range retrieved {
			if d != nil && strings.TrimSpace(d.DocType) == DocTypeClaim {
				lines += "- " + trimToChars(d.Text, 500) + "\n"
			}
		}
		return trimToTokens(lines, tokenBudget), nil
	}

	var entities []*types.ChatEntity
	_ = db.WithContext(dbc.Ctx).
		Model(&types.ChatEntity{}).
		Where("user_id = ? AND id IN ?", userID, entityIDs).
		Find(&entities).Error
	nameByID := map[uuid.UUID]string{}
	for _, e := range entities {
		if e != nil && e.ID != uuid.Nil && strings.TrimSpace(e.Name) != "" {
			nameByID[e.ID] = strings.TrimSpace(e.Name)
		}
	}

	var edges []*types.ChatEdge
	_ = db.WithContext(dbc.Ctx).
		Model(&types.ChatEdge{}).
		Where("user_id = ? AND (src_entity_id IN ? OR dst_entity_id IN ?)", userID, entityIDs, entityIDs).
		Order("created_at DESC").
		Limit(40).
		Find(&edges).Error

	lines := strings.Builder{}
	lines.WriteString("Entities:\n")
	for _, e := range entities {
		if e == nil {
			continue
		}
		lines.WriteString("- " + e.Name)
		if strings.TrimSpace(e.Type) != "" {
			lines.WriteString(" (" + strings.TrimSpace(e.Type) + ")")
		}
		if strings.TrimSpace(e.Description) != "" {
			lines.WriteString(": " + trimToChars(e.Description, 220))
		}
		lines.WriteString("\n")
	}

	if len(edges) > 0 {
		lines.WriteString("\nRelations:\n")
		for _, ed := range edges {
			if ed == nil {
				continue
			}
			src := "Unknown entity"
			if n, ok := nameByID[ed.SrcEntityID]; ok {
				src = n
			}
			dst := "Unknown entity"
			if n, ok := nameByID[ed.DstEntityID]; ok {
				dst = n
			}
			lines.WriteString("- " + src + " -" + strings.TrimSpace(ed.Relation) + "-> " + dst + "\n")
		}
	}

	// Add claim docs already retrieved (grounded).
	claims := ""
	for _, d := range retrieved {
		if d != nil && strings.TrimSpace(d.DocType) == DocTypeClaim {
			claims += "- " + trimToChars(d.Text, 500) + "\n"
		}
	}
	if strings.TrimSpace(claims) != "" {
		lines.WriteString("\nClaims:\n")
		lines.WriteString(claims)
	}

	return trimToTokens(lines.String(), tokenBudget), nil
}

func docMetadata(d *types.ChatDoc) map[string]any {
	if d == nil {
		return map[string]any{}
	}
	md := map[string]any{
		"user_id":   d.UserID.String(),
		"doc_type":  strings.TrimSpace(d.DocType),
		"scope":     strings.TrimSpace(d.Scope),
		"chunk_idx": d.ChunkIndex,
	}
	if d.ScopeID != nil && *d.ScopeID != uuid.Nil {
		md["scope_id"] = d.ScopeID.String()
	}
	if d.ThreadID != nil && *d.ThreadID != uuid.Nil {
		md["thread_id"] = d.ThreadID.String()
	}
	if d.PathID != nil && *d.PathID != uuid.Nil {
		md["path_id"] = d.PathID.String()
	}
	if d.JobID != nil && *d.JobID != uuid.Nil {
		md["job_id"] = d.JobID.String()
	}
	return md
}

func upsertVectors(ctx context.Context, vec pc.VectorStore, namespace string, docs []*types.ChatDoc, embeddings [][]float32) error {
	if vec == nil || len(docs) == 0 {
		return nil
	}
	if len(embeddings) != len(docs) {
		return fmt.Errorf("upsertVectors: embeddings mismatch")
	}
	v := make([]pc.Vector, 0, len(docs))
	for i, d := range docs {
		if d == nil || len(embeddings[i]) == 0 {
			continue
		}
		v = append(v, pc.Vector{
			ID:       d.VectorID,
			Values:   embeddings[i],
			Metadata: docMetadata(d),
		})
	}
	if len(v) == 0 {
		return nil
	}
	return vec.Upsert(ctx, namespace, v)
}
