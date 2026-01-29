package steps

import (
	"context"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/index"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"golang.org/x/sync/errgroup"
)

func semanticMatchCanonicalConcepts(ctx context.Context, deps ConceptGraphBuildDeps, concepts []conceptInvItem, embs [][]float32, signals AdaptiveSignals, contentType string, adaptiveEnabled bool, progress func(done, total int)) (map[string]uuid.UUID, map[string]any) {
	out := map[string]uuid.UUID{}
	params := map[string]any{}
	if deps.Vec == nil || deps.Concepts == nil || len(concepts) == 0 || len(embs) != len(concepts) {
		return out, params
	}

	minScore := envFloatAllowZero("CANONICAL_CONCEPT_SEMANTIC_MIN_SCORE", 0.885)
	if adaptiveEnabled {
		minScore = clamp01(adjustThresholdByContentType("CANONICAL_CONCEPT_SEMANTIC_MIN_SCORE", minScore, contentType))
	}
	params["CANONICAL_CONCEPT_SEMANTIC_MIN_SCORE"] = map[string]any{"actual": minScore}
	minGap := envFloatAllowZero("CANONICAL_CONCEPT_SEMANTIC_MIN_GAP", 0.02)
	if adaptiveEnabled {
		switch strings.ToLower(strings.TrimSpace(contentType)) {
		case "slides", "mixed":
			minGap = minGap + 0.01
		case "prose":
			minGap = minGap - 0.005
		case "code":
			minGap = minGap - 0.003
		}
	}
	if minGap < 0 {
		minGap = 0
	}
	if minGap > 0.2 {
		minGap = 0.2
	}
	params["CANONICAL_CONCEPT_SEMANTIC_MIN_GAP"] = map[string]any{"actual": minGap}
	topK := envIntAllowZero("CANONICAL_CONCEPT_SEMANTIC_TOP_K", 6)
	topKCeiling := topK
	if topK <= 0 {
		topK = 6
		topKCeiling = topK
	}
	if adaptiveEnabled && signals.ConceptCount > 0 {
		raw := int(math.Round(float64(signals.ConceptCount)/100.0)) + 4
		topK = clampIntCeiling(raw, 4, topKCeiling)
	}
	params["CANONICAL_CONCEPT_SEMANTIC_TOP_K"] = map[string]any{"actual": topK, "ceiling": topKCeiling}
	conc := envIntAllowZero("CANONICAL_CONCEPT_SEMANTIC_CONCURRENCY", 32)
	if conc < 1 {
		conc = 1
	}
	timeoutMS := envIntAllowZero("CANONICAL_CONCEPT_SEMANTIC_TIMEOUT_MS", 2500)
	if timeoutMS < 250 {
		timeoutMS = 250
	}

	keys := make([]string, 0, len(concepts))
	aliasKeysByKey := map[string][]string{}
	aliasKeys := make([]string, 0, len(concepts)*2)
	for _, c := range concepts {
		k := strings.TrimSpace(strings.ToLower(c.Key))
		if k == "" {
			continue
		}
		keys = append(keys, k)
		for _, a := range c.Aliases {
			ak := normalizeConceptKey(a)
			if ak == "" || ak == k {
				continue
			}
			aliasKeysByKey[k] = append(aliasKeysByKey[k], ak)
			aliasKeys = append(aliasKeys, ak)
		}
	}
	keys = dedupeStrings(keys)
	queryKeys := dedupeStrings(append(keys, aliasKeys...))

	globalRootByKey := map[string]uuid.UUID{}
	if len(queryKeys) > 0 {
		if rows, err := deps.Concepts.GetByScopeAndKeys(dbctx.Context{Ctx: ctx}, "global", nil, queryKeys); err == nil {
			for _, g := range rows {
				if g == nil || g.ID == uuid.Nil {
					continue
				}
				k := strings.TrimSpace(strings.ToLower(g.Key))
				if k == "" {
					continue
				}
				root := g.ID
				if g.CanonicalConceptID != nil && *g.CanonicalConceptID != uuid.Nil {
					root = *g.CanonicalConceptID
				}
				if root != uuid.Nil {
					globalRootByKey[k] = root
				}
			}
		}
	}

	todoIdx := make([]int, 0, 8)
	aliasMatched := 0
	for i := range concepts {
		k := strings.TrimSpace(strings.ToLower(concepts[i].Key))
		if k == "" {
			continue
		}
		if globalRootByKey[k] != uuid.Nil {
			continue // exact global key already exists
		}
		aks := dedupeStrings(aliasKeysByKey[k])
		found := uuid.Nil
		for _, ak := range aks {
			if id := globalRootByKey[ak]; id != uuid.Nil {
				found = id
				break
			}
		}
		if found != uuid.Nil {
			out[k] = found
			aliasMatched++
			continue
		}
		todoIdx = append(todoIdx, i)
	}

	semanticMatched := 0
	if minScore > 0 && len(todoIdx) > 0 {
		if progress != nil {
			progress(0, len(todoIdx))
		}
		globalNS := index.ConceptsNamespace("global", nil)
		filter := map[string]any{"type": "concept", "scope": "global", "canonical": true}

		var mu sync.Mutex
		var doneCount int32
		eg, egctx := errgroup.WithContext(ctx)
		eg.SetLimit(conc)

		for _, idx := range todoIdx {
			idx := idx
			eg.Go(func() error {
				defer func() {
					if progress != nil {
						done := int(atomic.AddInt32(&doneCount, 1))
						progress(done, len(todoIdx))
					}
				}()
				if idx < 0 || idx >= len(concepts) || idx >= len(embs) {
					return nil
				}
				k := strings.TrimSpace(strings.ToLower(concepts[idx].Key))
				if k == "" {
					return nil
				}
				if len(embs[idx]) == 0 {
					return nil
				}
				qctx, cancel := context.WithTimeout(egctx, time.Duration(timeoutMS)*time.Millisecond)
				matches, err := deps.Vec.QueryMatches(qctx, globalNS, embs[idx], topK, filter)
				cancel()
				if err != nil || len(matches) == 0 {
					return nil
				}
				best := matches[0]
				if best.Score < minScore {
					return nil
				}
				if len(matches) > 1 && (best.Score-matches[1].Score) < minGap {
					return nil
				}
				idStr := strings.TrimSpace(best.ID)
				if strings.HasPrefix(idStr, "concept:") {
					idStr = strings.TrimPrefix(idStr, "concept:")
				}
				cid, err := uuid.Parse(strings.TrimSpace(idStr))
				if err != nil || cid == uuid.Nil {
					return nil
				}
				mu.Lock()
				out[k] = cid
				semanticMatched++
				mu.Unlock()
				return nil
			})
		}
		_ = eg.Wait()
	}

	if len(out) > 0 {
		ids := make([]uuid.UUID, 0, len(out))
		seenIDs := map[uuid.UUID]bool{}
		for _, id := range out {
			if id != uuid.Nil && !seenIDs[id] {
				seenIDs[id] = true
				ids = append(ids, id)
			}
		}
		if len(ids) > 0 {
			if rows, err := deps.Concepts.GetByIDs(dbctx.Context{Ctx: ctx}, ids); err == nil {
				redir := map[uuid.UUID]uuid.UUID{}
				for _, r := range rows {
					if r == nil || r.ID == uuid.Nil {
						continue
					}
					if r.CanonicalConceptID != nil && *r.CanonicalConceptID != uuid.Nil {
						redir[r.ID] = *r.CanonicalConceptID
					}
				}
				if len(redir) > 0 {
					for k, id := range out {
						if to := redir[id]; to != uuid.Nil {
							out[k] = to
						}
					}
				}
			}
		}
	}

	if deps.Log != nil && (aliasMatched > 0 || semanticMatched > 0) {
		deps.Log.Info(
			"canonical concept semantic matches",
			"alias_matches", aliasMatched,
			"semantic_matches", semanticMatched,
			"candidates", len(concepts),
		)
	}

	return out, params
}
