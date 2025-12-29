package user_model_update

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
)

func (p *Pipeline) applyQuestionAnswered(dbc dbctx.Context, userID uuid.UUID, ev *types.UserEvent, data map[string]any) error {
	if p == nil || p.conceptState == nil {
		return nil
	}
	if userID == uuid.Nil || ev == nil {
		return nil
	}

	conceptIDs := extractUUIDsFromAny(data["concept_ids"])
	if ev.ConceptID != nil && *ev.ConceptID != uuid.Nil && len(conceptIDs) == 0 {
		conceptIDs = []uuid.UUID{*ev.ConceptID}
	}
	if len(conceptIDs) == 0 {
		return nil
	}

	isCorrect := boolFromAny(data["is_correct"], false)
	latencyMS := intFromAny(data["latency_ms"], 0)

	seenAt := ev.OccurredAt
	if seenAt.IsZero() {
		seenAt = time.Now().UTC()
	}

	for _, cid := range conceptIDs {
		if cid == uuid.Nil {
			continue
		}

		prev, _ := p.conceptState.Get(dbc, userID, cid)

		m := 0.0
		c := 0.0
		if prev != nil {
			m = clamp01(prev.Mastery)
			c = clamp01(prev.Confidence)
		}

		// Small, stable update. Slow answers get slightly smaller positive step.
		alpha := 0.06
		if latencyMS > 0 && latencyMS > 12000 {
			alpha = 0.04
		}

		if isCorrect {
			m = m + (1.0-m)*alpha
			c = c + (1.0-c)*0.05
		} else {
			m = m - m*0.10
			c = c - c*0.10
		}

		m = clamp01(m)
		c = clamp01(c)

		_ = p.conceptState.UpsertDelta(dbc, userID, cid, m, c, &seenAt)
	}

	return nil
}

// ---- helpers ----

// extractUUIDsFromAny supports []any, []string, single string, etc.
func extractUUIDsFromAny(v any) []uuid.UUID {
	if v == nil {
		return nil
	}

	// []string
	if ss, ok := v.([]string); ok {
		out := make([]uuid.UUID, 0, len(ss))
		for _, s := range ss {
			id, err := uuid.Parse(strings.TrimSpace(s))
			if err == nil && id != uuid.Nil {
				out = append(out, id)
			}
		}
		return out
	}

	// []any
	if arr, ok := v.([]any); ok {
		out := make([]uuid.UUID, 0, len(arr))
		for _, x := range arr {
			id, err := uuid.Parse(strings.TrimSpace(fmt.Sprint(x)))
			if err == nil && id != uuid.Nil {
				out = append(out, id)
			}
		}
		return out
	}

	// single string/uuid-like
	s := strings.TrimSpace(fmt.Sprint(v))
	if s == "" {
		return nil
	}
	if id, err := uuid.Parse(s); err == nil && id != uuid.Nil {
		return []uuid.UUID{id}
	}
	return nil
}

func boolFromAny(v any, def bool) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		s := strings.ToLower(strings.TrimSpace(t))
		return s == "true" || s == "1" || s == "yes"
	case float64:
		return t != 0
	default:
		return def
	}
}

func intFromAny(v any, def int) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case json.Number:
		i, _ := t.Int64()
		return int(i)
	default:
		return def
	}
}

func floatFromAny(v any, def float64) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case float32:
		return float64(t)
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case json.Number:
		f, err := t.Float64()
		if err == nil {
			return f
		}
		return def
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return def
		}
		n := json.Number(s)
		f, err := n.Float64()
		if err == nil {
			return f
		}
		return def
	default:
		return def
	}
}

func clamp01(x float64) float64 {
	if math.IsNaN(x) {
		return 0
	}
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}
