package runtime_update

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

type runtimePrompt struct {
	ID              string  `json:"id"`
	Type            string  `json:"type"`
	NodeID          string  `json:"node_id,omitempty"`
	BlockID         string  `json:"block_id,omitempty"`
	Reason          string  `json:"reason,omitempty"`
	Status          string  `json:"status,omitempty"`
	CreatedAt       string  `json:"created_at,omitempty"`
	DecisionTraceID string  `json:"decision_trace_id,omitempty"`
	PolicyKey       string  `json:"policy_key,omitempty"`
	PolicyMode      string  `json:"policy_mode,omitempty"`
	PolicyVersion   int     `json:"policy_version,omitempty"`
	BehaviorProb    float64 `json:"behavior_prob,omitempty"`
	ShadowProb      float64 `json:"shadow_prob,omitempty"`
}

type runtimePolicy struct {
	MaxPromptsPerHour int
	BreakAfterMinutes int
	BreakMinMinutes   int
	BreakMaxMinutes   int

	QuickCheckAfterBlocks  int
	QuickCheckAfterMinutes int
	QuickCheckMaxPerLesson int
	QuickCheckMinGapBlocks int

	FlashcardAfterBlocks     int
	FlashcardAfterMinutes    int
	FlashcardAfterFailStreak int
	FlashcardMaxPerLesson    int
}

const (
	defaultMaxPromptsPerHour  = 8
	defaultBreakAfterMinutes  = 25
	defaultBreakMinMinutes    = 3
	defaultBreakMaxMinutes    = 8
	defaultQCAboveBlocks      = 3
	defaultQCAfterMinutes     = 6
	defaultQCMaxPerLesson     = 4
	defaultQCMinGapBlocks     = 1
	defaultFCAfterBlocks      = 4
	defaultFCAfterMinutes     = 8
	defaultFCAfterFailStreak  = 2
	defaultFCMaxPerLesson     = 6
	runtimePromptMinGapMinute = 2
	defaultProgressConfMin    = 0.6
)

func decodeJSONMap(raw datatypes.JSON) map[string]any {
	out := map[string]any{}
	if len(raw) == 0 || string(raw) == "null" {
		return out
	}
	_ = json.Unmarshal(raw, &out)
	if out == nil {
		out = map[string]any{}
	}
	return out
}

func encodeJSONMap(m map[string]any) datatypes.JSON {
	if m == nil {
		return datatypes.JSON([]byte("null"))
	}
	b, _ := json.Marshal(m)
	return datatypes.JSON(b)
}

func mapFromAny(v any) map[string]any {
	if v == nil {
		return map[string]any{}
	}
	if m, ok := v.(map[string]any); ok && m != nil {
		return m
	}
	if m, ok := v.(map[string]interface{}); ok && m != nil {
		out := map[string]any{}
		for k, v := range m {
			out[k] = v
		}
		return out
	}
	if raw, ok := v.(json.RawMessage); ok && len(raw) > 0 {
		out := map[string]any{}
		_ = json.Unmarshal(raw, &out)
		return out
	}
	if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
		out := map[string]any{}
		_ = json.Unmarshal([]byte(s), &out)
		return out
	}
	return map[string]any{}
}

func stringFromAny(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case fmt.Stringer:
		return strings.TrimSpace(t.String())
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func intFromAny(v any, fallback int) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case float32:
		return int(t)
	case string:
		if t == "" {
			return fallback
		}
		var n int
		if _, err := fmt.Sscanf(t, "%d", &n); err == nil {
			return n
		}
	}
	return fallback
}

func floatFromAny(v any, fallback float64) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case float32:
		return float64(t)
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case string:
		if t == "" {
			return fallback
		}
		if n, err := strconv.ParseFloat(strings.TrimSpace(t), 64); err == nil {
			return n
		}
	}
	return fallback
}

func boolFromAny(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "true", "1", "yes", "y":
			return true
		}
	case float64:
		return t != 0
	case int:
		return t != 0
	}
	return false
}

func timeFromAny(v any) *time.Time {
	switch t := v.(type) {
	case time.Time:
		return &t
	case *time.Time:
		return t
	case string:
		if strings.TrimSpace(t) == "" {
			return nil
		}
		if ts, err := time.Parse(time.RFC3339, t); err == nil {
			return &ts
		}
	}
	return nil
}

func progressConfMin() float64 {
	raw := strings.TrimSpace(os.Getenv("RUNTIME_PROGRESS_CONF_MIN"))
	if raw == "" {
		return defaultProgressConfMin
	}
	if v, err := strconv.ParseFloat(raw, 64); err == nil && v > 0 {
		return v
	}
	return defaultProgressConfMin
}

func progressEligible(state string, conf float64) bool {
	state = strings.ToLower(strings.TrimSpace(state))
	if state == "" && conf <= 0 {
		return true
	}
	if state != "progressing" {
		return false
	}
	if conf <= 0 {
		return false
	}
	return conf >= progressConfMin()
}

func allowReshowUncompleted() bool {
	raw := strings.TrimSpace(os.Getenv("RUNTIME_RESHOW_UNCOMPLETED_PROMPTS"))
	if raw == "" {
		return false
	}
	switch strings.ToLower(raw) {
	case "1", "true", "yes", "y":
		return true
	default:
		return false
	}
}

func stringSliceFromAny(v any) []string {
	out := []string{}
	switch t := v.(type) {
	case []string:
		out = append(out, t...)
	case []any:
		for _, x := range t {
			s := stringFromAny(x)
			if s != "" {
				out = append(out, s)
			}
		}
	}
	seen := map[string]bool{}
	norm := make([]string, 0, len(out))
	for _, s := range out {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		norm = append(norm, s)
	}
	return norm
}

func getRuntimePlan(meta map[string]any) map[string]any {
	if meta == nil {
		return nil
	}
	if raw, ok := meta["runtime_plan"]; ok {
		plan := mapFromAny(raw)
		if len(plan) > 0 {
			return plan
		}
	}
	return nil
}

func resolveRuntimePolicy(plan map[string]any, nodeID uuid.UUID) runtimePolicy {
	p := runtimePolicy{
		MaxPromptsPerHour:        defaultMaxPromptsPerHour,
		BreakAfterMinutes:        defaultBreakAfterMinutes,
		BreakMinMinutes:          defaultBreakMinMinutes,
		BreakMaxMinutes:          defaultBreakMaxMinutes,
		QuickCheckAfterBlocks:    defaultQCAboveBlocks,
		QuickCheckAfterMinutes:   defaultQCAfterMinutes,
		QuickCheckMaxPerLesson:   defaultQCMaxPerLesson,
		QuickCheckMinGapBlocks:   defaultQCMinGapBlocks,
		FlashcardAfterBlocks:     defaultFCAfterBlocks,
		FlashcardAfterMinutes:    defaultFCAfterMinutes,
		FlashcardAfterFailStreak: defaultFCAfterFailStreak,
		FlashcardMaxPerLesson:    defaultFCMaxPerLesson,
	}
	if plan == nil {
		return p
	}
	pathPolicy := mapFromAny(plan["path"])
	if len(pathPolicy) > 0 {
		p.MaxPromptsPerHour = intFromAny(pathPolicy["max_prompts_per_hour"], p.MaxPromptsPerHour)
		if bp := mapFromAny(pathPolicy["break_policy"]); len(bp) > 0 {
			p.BreakAfterMinutes = intFromAny(bp["after_minutes"], p.BreakAfterMinutes)
			p.BreakMinMinutes = intFromAny(bp["min_break_minutes"], p.BreakMinMinutes)
			p.BreakMaxMinutes = intFromAny(bp["max_break_minutes"], p.BreakMaxMinutes)
		}
		if qp := mapFromAny(pathPolicy["quick_check_policy"]); len(qp) > 0 {
			p.QuickCheckAfterBlocks = intFromAny(qp["after_blocks"], p.QuickCheckAfterBlocks)
			p.QuickCheckAfterMinutes = intFromAny(qp["after_minutes"], p.QuickCheckAfterMinutes)
			p.QuickCheckMaxPerLesson = intFromAny(qp["max_per_lesson"], p.QuickCheckMaxPerLesson)
			p.QuickCheckMinGapBlocks = intFromAny(qp["min_gap_blocks"], p.QuickCheckMinGapBlocks)
		}
		if fp := mapFromAny(pathPolicy["flashcard_policy"]); len(fp) > 0 {
			p.FlashcardAfterBlocks = intFromAny(fp["after_blocks"], p.FlashcardAfterBlocks)
			p.FlashcardAfterMinutes = intFromAny(fp["after_minutes"], p.FlashcardAfterMinutes)
			p.FlashcardAfterFailStreak = intFromAny(fp["after_fail_streak"], p.FlashcardAfterFailStreak)
			p.FlashcardMaxPerLesson = intFromAny(fp["max_per_lesson"], p.FlashcardMaxPerLesson)
		}
	}

	lessons := mapFromAny(plan)["lessons"]
	if lessonsArr, ok := lessons.([]any); ok && nodeID != uuid.Nil {
		nodeIDStr := nodeID.String()
		for _, raw := range lessonsArr {
			m := mapFromAny(raw)
			if m == nil {
				continue
			}
			if strings.TrimSpace(stringFromAny(m["node_id"])) != nodeIDStr {
				continue
			}
			if bp := mapFromAny(m["break_policy"]); len(bp) > 0 {
				p.BreakAfterMinutes = intFromAny(bp["after_minutes"], p.BreakAfterMinutes)
				p.BreakMinMinutes = intFromAny(bp["min_break_minutes"], p.BreakMinMinutes)
				p.BreakMaxMinutes = intFromAny(bp["max_break_minutes"], p.BreakMaxMinutes)
			}
			if qp := mapFromAny(m["quick_check_policy"]); len(qp) > 0 {
				p.QuickCheckAfterBlocks = intFromAny(qp["after_blocks"], p.QuickCheckAfterBlocks)
				p.QuickCheckAfterMinutes = intFromAny(qp["after_minutes"], p.QuickCheckAfterMinutes)
				p.QuickCheckMaxPerLesson = intFromAny(qp["max_per_lesson"], p.QuickCheckMaxPerLesson)
				p.QuickCheckMinGapBlocks = intFromAny(qp["min_gap_blocks"], p.QuickCheckMinGapBlocks)
			}
			if fp := mapFromAny(m["flashcard_policy"]); len(fp) > 0 {
				p.FlashcardAfterBlocks = intFromAny(fp["after_blocks"], p.FlashcardAfterBlocks)
				p.FlashcardAfterMinutes = intFromAny(fp["after_minutes"], p.FlashcardAfterMinutes)
				p.FlashcardAfterFailStreak = intFromAny(fp["after_fail_streak"], p.FlashcardAfterFailStreak)
				p.FlashcardMaxPerLesson = intFromAny(fp["max_per_lesson"], p.FlashcardMaxPerLesson)
			}
			break
		}
	}
	return p
}

func runtimePromptFromMap(m map[string]any) *runtimePrompt {
	if m == nil {
		return nil
	}
	id := stringFromAny(m["id"])
	if id == "" {
		return nil
	}
	return &runtimePrompt{
		ID:              id,
		Type:            stringFromAny(m["type"]),
		NodeID:          stringFromAny(m["node_id"]),
		BlockID:         stringFromAny(m["block_id"]),
		Reason:          stringFromAny(m["reason"]),
		Status:          stringFromAny(m["status"]),
		CreatedAt:       stringFromAny(m["created_at"]),
		DecisionTraceID: stringFromAny(m["decision_trace_id"]),
		PolicyKey:       stringFromAny(m["policy_key"]),
		PolicyMode:      stringFromAny(m["policy_mode"]),
		PolicyVersion:   intFromAny(m["policy_version"], 0),
		BehaviorProb:    floatFromAny(m["behavior_prob"], 0),
		ShadowProb:      floatFromAny(m["shadow_prob"], 0),
	}
}

func runtimePromptToMap(p runtimePrompt) map[string]any {
	return map[string]any{
		"id":                p.ID,
		"type":              p.Type,
		"node_id":           p.NodeID,
		"block_id":          p.BlockID,
		"reason":            p.Reason,
		"status":            p.Status,
		"created_at":        p.CreatedAt,
		"decision_trace_id": p.DecisionTraceID,
		"policy_key":        p.PolicyKey,
		"policy_mode":       p.PolicyMode,
		"policy_version":    p.PolicyVersion,
		"behavior_prob":     p.BehaviorProb,
		"shadow_prob":       p.ShadowProb,
	}
}

func shouldResetPromptWindow(start *time.Time, now time.Time) bool {
	if start == nil {
		return true
	}
	return now.Sub(*start) > time.Hour
}
