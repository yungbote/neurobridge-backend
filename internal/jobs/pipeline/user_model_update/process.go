package user_model_update

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
)

const (
	defaultDecayRate      = 0.015
	maxExposureConfidence = 0.35
)

func ensureConceptState(prev *types.UserConceptState, userID uuid.UUID, conceptID uuid.UUID) *types.UserConceptState {
	if prev != nil {
		return prev
	}
	return &types.UserConceptState{
		ID:         uuid.New(),
		UserID:     userID,
		ConceptID:  conceptID,
		Mastery:    0,
		Confidence: 0,
		DecayRate:  defaultDecayRate,
		Attempts:   0,
		Correct:    0,
	}
}

func setLastSeen(st *types.UserConceptState, seenAt time.Time) {
	if st == nil || seenAt.IsZero() {
		return
	}
	if st.LastSeenAt == nil || st.LastSeenAt.IsZero() || seenAt.After(*st.LastSeenAt) {
		t := seenAt.UTC()
		st.LastSeenAt = &t
	}
}

func applyQuestionAnsweredToState(st *types.UserConceptState, seenAt time.Time, data map[string]any) {
	if st == nil {
		return
	}

	isCorrect := boolFromAny(data["is_correct"], false)
	latencyMS := intFromAny(data["latency_ms"], 0)
	selfConf := clamp01(floatFromAny(data["confidence"], 0))

	// Attempts/correct counters (queryable evidence).
	st.Attempts += 1
	if isCorrect {
		st.Correct += 1
	}

	m := clamp01(st.Mastery)
	c := clamp01(st.Confidence)

	// Stable learning-rate: scale by self-reported confidence and latency.
	alpha := 0.08 * (0.75 + 0.5*selfConf)
	if latencyMS > 0 && latencyMS > 12000 {
		alpha *= 0.75
	}
	alpha = clamp01(alpha)

	if isCorrect {
		m = m + (1.0-m)*alpha
		c = c + (1.0-c)*0.06
	} else {
		m = m - m*0.12
		c = c - c*0.08
	}

	st.Mastery = clamp01(m)
	st.Confidence = clamp01(c)

	setLastSeen(st, seenAt)

	// Simple review scheduling: incorrect => soon; correct => later as mastery rises.
	now := seenAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	strength := clamp01(st.Mastery * st.Confidence)
	days := 0.25 + (strength * strength * 28.0)
	if !isCorrect {
		days = 0.25
	}
	if days < 0.10 {
		days = 0.10
	}
	if days > 60 {
		days = 60
	}
	next := now.Add(time.Duration(days*24) * time.Hour).UTC()
	st.NextReviewAt = &next

	// Decay rate is a coarse signal for future spaced repetition logic.
	st.DecayRate = clamp01(defaultDecayRate + (1.0-strength)*0.01)

	// Track a small rolling misconception log on incorrect answers (best-effort).
	if !isCorrect {
		var arr []map[string]any
		if len(st.Misconceptions) > 0 && string(st.Misconceptions) != "null" {
			_ = json.Unmarshal(st.Misconceptions, &arr)
		}
		arr = append(arr, map[string]any{
			"kind":        "incorrect_answer",
			"question_id": strings.TrimSpace(fmt.Sprint(data["question_id"])),
			"selected_id": strings.TrimSpace(fmt.Sprint(data["selected_id"])),
			"answer_id":   strings.TrimSpace(fmt.Sprint(data["answer_id"])),
			"occurred_at": now.UTC().Format(time.RFC3339Nano),
			"confidence":  selfConf,
			"latency_ms":  latencyMS,
		})
		if len(arr) > 20 {
			arr = arr[len(arr)-20:]
		}
		if b, err := json.Marshal(arr); err == nil {
			st.Misconceptions = datatypes.JSON(b)
		}
	}
}

func applyActivityCompletedToState(st *types.UserConceptState, seenAt time.Time, data map[string]any) {
	if st == nil {
		return
	}
	score := clamp01(floatFromAny(data["score"], 0))
	if score == 0 {
		score = 0.6 // weak positive default when completion has no explicit score
	}

	m := clamp01(st.Mastery)
	c := clamp01(st.Confidence)

	// Completion is weaker evidence than assessment; nudge upward conservatively.
	alpha := 0.02 + 0.04*score
	m = m + (1.0-m)*(alpha*0.60)
	c = c + (1.0-c)*(0.02*score)

	st.Mastery = clamp01(m)
	st.Confidence = clamp01(c)
	setLastSeen(st, seenAt)
}

func applyExposureToState(st *types.UserConceptState, seenAt time.Time, data map[string]any) {
	if st == nil {
		return
	}
	dwellMS := float64(intFromAny(data["dwell_ms"], 0))
	maxPct := clamp01(floatFromAny(data["max_percent"], floatFromAny(data["percent"], 0)) / 100.0)
	if maxPct <= 0 {
		maxPct = 0.3
	}
	// Scale exposure weight by dwell time (cap at ~2 minutes) and scroll depth.
	w := clamp01((dwellMS / 120000.0) * maxPct)
	c := clamp01(st.Confidence)
	c = clamp01(c + 0.06*w)
	if c > maxExposureConfidence {
		c = maxExposureConfidence
	}
	st.Confidence = c
	setLastSeen(st, seenAt)
}

func applyHintUsedToState(st *types.UserConceptState, seenAt time.Time, data map[string]any) {
	if st == nil {
		return
	}

	m := clamp01(st.Mastery)
	c := clamp01(st.Confidence)

	// Hints are a negative signal: user needed help to proceed.
	//
	// We penalize confidence more than mastery (hints imply uncertainty). The penalty is larger when
	// the user's current strength is low, and smaller when they're already confident/mastered.
	strength := clamp01(m * c)
	penalty := 0.03 + 0.05*(1.0-strength)
	c = clamp01(c - penalty)

	// If the user was previously "very mastered" but still needed hints, nudge mastery down slightly.
	if m >= 0.90 {
		m = clamp01(m - 0.02)
	}

	st.Mastery = m
	st.Confidence = c
	setLastSeen(st, seenAt)

	// Schedule near-term review (but do not delay an already-sooner review).
	now := seenAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	soon := now.Add(6 * time.Hour).UTC()
	if st.NextReviewAt == nil || st.NextReviewAt.IsZero() || soon.Before(*st.NextReviewAt) {
		st.NextReviewAt = &soon
	}

	// Track a small rolling "help needed" log for prompt-time misconception hints (best-effort).
	var arr []map[string]any
	if len(st.Misconceptions) > 0 && string(st.Misconceptions) != "null" {
		_ = json.Unmarshal(st.Misconceptions, &arr)
	}
	arr = append(arr, map[string]any{
		"kind":        "hint_used",
		"question_id": strings.TrimSpace(fmt.Sprint(data["question_id"])),
		"block_id":    strings.TrimSpace(fmt.Sprint(data["block_id"])),
		"occurred_at": now.UTC().Format(time.RFC3339Nano),
	})
	if len(arr) > 20 {
		arr = arr[len(arr)-20:]
	}
	if b, err := json.Marshal(arr); err == nil {
		st.Misconceptions = datatypes.JSON(b)
	}
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
