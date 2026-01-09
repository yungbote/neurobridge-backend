package steps

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/google/uuid"

	pc "github.com/yungbote/neurobridge-backend/internal/clients/pinecone"
	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/learning/index"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
)

type teachingPatternForPrompt struct {
	PatternKey     string         `json:"pattern_key"`
	Name           string         `json:"name"`
	WhenToUse      string         `json:"when_to_use"`
	Representation map[string]any `json:"representation,omitempty"`
	Constraints    map[string]any `json:"constraints,omitempty"`
	Score          float64        `json:"score,omitempty"`
}

func teachingPatternsJSON(ctx context.Context, vec pc.VectorStore, repo repos.TeachingPatternRepo, queryEmb []float32, limit int) (string, []teachingPatternForPrompt) {
	selected := selectTeachingPatterns(ctx, vec, repo, queryEmb, limit)
	if len(selected) == 0 {
		return "", nil
	}
	b, _ := json.Marshal(map[string]any{"patterns": selected})
	if len(b) == 0 || string(b) == "null" {
		return "", nil
	}
	return string(b), selected
}

func selectTeachingPatterns(ctx context.Context, vec pc.VectorStore, repo repos.TeachingPatternRepo, queryEmb []float32, limit int) []teachingPatternForPrompt {
	if repo == nil {
		return nil
	}
	if limit <= 0 {
		limit = 4
	}
	if limit > 8 {
		limit = 8
	}

	type scoredKey struct {
		Key   string
		Score float64
	}

	keys := make([]scoredKey, 0, limit)
	if vec != nil && len(queryEmb) > 0 {
		matches, err := vec.QueryMatches(ctx, index.TeachingPatternsNamespace(), queryEmb, limit, map[string]any{"type": "teaching_pattern"})
		if err == nil && len(matches) > 0 {
			for _, m := range matches {
				id := strings.TrimSpace(m.ID)
				if id == "" {
					continue
				}
				key := strings.TrimSpace(strings.TrimPrefix(id, "teaching_pattern:"))
				if key == "" {
					continue
				}
				keys = append(keys, scoredKey{Key: key, Score: m.Score})
			}
		}
	}

	// Stable fallback when the vector store isn't configured.
	if len(keys) == 0 {
		rows, err := repo.ListAll(dbctx.Context{Ctx: ctx}, limit)
		if err != nil || len(rows) == 0 {
			return nil
		}
		out := make([]teachingPatternForPrompt, 0, len(rows))
		for _, r := range rows {
			if r == nil || strings.TrimSpace(r.PatternKey) == "" {
				continue
			}
			rep, con := parseTeachingPatternSpec(r.PatternSpec)
			out = append(out, teachingPatternForPrompt{
				PatternKey:     strings.TrimSpace(r.PatternKey),
				Name:           strings.TrimSpace(r.Name),
				WhenToUse:      strings.TrimSpace(r.WhenToUse),
				Representation: rep,
				Constraints:    con,
			})
		}
		return out
	}

	seen := map[string]bool{}
	out := make([]teachingPatternForPrompt, 0, len(keys))
	for _, sk := range keys {
		k := strings.TrimSpace(sk.Key)
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		row, err := repo.GetByPatternKey(dbctx.Context{Ctx: ctx}, k)
		if err != nil || row == nil || strings.TrimSpace(row.PatternKey) == "" {
			continue
		}
		rep, con := parseTeachingPatternSpec(row.PatternSpec)
		out = append(out, teachingPatternForPrompt{
			PatternKey:     strings.TrimSpace(row.PatternKey),
			Name:           strings.TrimSpace(row.Name),
			WhenToUse:      strings.TrimSpace(row.WhenToUse),
			Representation: rep,
			Constraints:    con,
			Score:          sk.Score,
		})
		if len(out) >= limit {
			break
		}
	}

	// Deterministic order for prompt stability.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].PatternKey < out[j].PatternKey
	})
	return out
}

func parseTeachingPatternSpec(raw []byte) (representation map[string]any, constraints map[string]any) {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "" || strings.TrimSpace(string(raw)) == "null" {
		return nil, nil
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return nil, nil
	}
	rep, _ := obj["representation"].(map[string]any)
	con, _ := obj["constraints"].(map[string]any)
	return rep, con
}

func uuidPtr(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}
