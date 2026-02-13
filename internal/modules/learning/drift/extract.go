package drift

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"gorm.io/datatypes"
)

func extractCandidateScores(raw datatypes.JSON) []float64 {
	val := decodeJSON(raw)
	if val == nil {
		return nil
	}
	scores := collectScores(val, 0)
	if len(scores) == 0 {
		return nil
	}
	out := make([]float64, 0, len(scores))
	for _, s := range scores {
		if !mathIsNaNOrInf(s) {
			out = append(out, s)
		}
	}
	return out
}

func extractChosenScore(raw datatypes.JSON) float64 {
	val := decodeJSON(raw)
	if val == nil {
		return 0
	}
	if score, ok := findScoreValue(val, 0); ok {
		return score
	}
	return 0
}

func extractThreshold(raw datatypes.JSON) (float64, bool) {
	val := decodeJSON(raw)
	if val == nil {
		return 0, false
	}
	if thr, ok := findThresholdValue(val, 0); ok {
		return thr, true
	}
	return 0, false
}

func decodeJSON(raw datatypes.JSON) any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil
	}
	return v
}

func collectScores(v any, depth int) []float64 {
	if depth > 4 {
		return nil
	}
	switch t := v.(type) {
	case []any:
		out := []float64{}
		for _, item := range t {
			out = append(out, collectScores(item, depth+1)...)
		}
		return out
	case map[string]any:
		out := []float64{}
		if score, ok := scoreFromMap(t); ok {
			out = append(out, score)
		}
		for k, val := range t {
			lk := strings.ToLower(strings.TrimSpace(k))
			if lk == "candidates" || lk == "options" || lk == "alternatives" || lk == "choices" {
				out = append(out, collectScores(val, depth+1)...)
			}
		}
		for _, val := range t {
			out = append(out, collectScores(val, depth+1)...)
		}
		return out
	default:
		if f, ok := floatFromAnyRaw(t); ok {
			return []float64{f}
		}
	}
	return nil
}

func scoreFromMap(m map[string]any) (float64, bool) {
	if m == nil {
		return 0, false
	}
	keys := []string{"score", "similarity", "confidence", "prob", "probability", "weight", "rank_score"}
	for _, key := range keys {
		if v, ok := m[key]; ok {
			if f, ok := floatFromAnyRaw(v); ok {
				return f, true
			}
		}
	}
	return 0, false
}

func findScoreValue(v any, depth int) (float64, bool) {
	if depth > 4 {
		return 0, false
	}
	switch t := v.(type) {
	case map[string]any:
		if score, ok := scoreFromMap(t); ok {
			return score, true
		}
		for _, val := range t {
			if score, ok := findScoreValue(val, depth+1); ok {
				return score, true
			}
		}
	case []any:
		for _, item := range t {
			if score, ok := findScoreValue(item, depth+1); ok {
				return score, true
			}
		}
	default:
		if f, ok := floatFromAnyRaw(t); ok {
			return f, true
		}
	}
	return 0, false
}

func findThresholdValue(v any, depth int) (float64, bool) {
	if depth > 4 {
		return 0, false
	}
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			lk := strings.ToLower(strings.TrimSpace(k))
			if strings.Contains(lk, "threshold") || strings.Contains(lk, "min_score") || strings.Contains(lk, "min_similarity") || strings.Contains(lk, "score_min") {
				if f, ok := floatFromAnyRaw(val); ok {
					return f, true
				}
			}
			if f, ok := findThresholdValue(val, depth+1); ok {
				return f, true
			}
		}
	case []any:
		for _, item := range t {
			if f, ok := findThresholdValue(item, depth+1); ok {
				return f, true
			}
		}
	default:
		if f, ok := floatFromAnyRaw(t); ok {
			return f, true
		}
	}
	return 0, false
}

func floatFromAnyRaw(v any) (float64, bool) {
	if v == nil {
		return 0, false
	}
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case int32:
		return float64(t), true
	case uint:
		return float64(t), true
	case uint64:
		return float64(t), true
	case uint32:
		return float64(t), true
	case json.Number:
		if f, err := t.Float64(); err == nil {
			return f, true
		}
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return 0, false
		}
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return f, true
		}
	default:
		s := strings.TrimSpace(fmt.Sprint(v))
		if s == "" {
			return 0, false
		}
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return f, true
		}
	}
	return 0, false
}

func mathIsNaNOrInf(v float64) bool {
	return math.IsNaN(v) || math.IsInf(v, 0)
}

func intFromAny(v any, def int) int {
	if v == nil {
		return def
	}
	s := strings.TrimSpace(fmt.Sprint(v))
	if s == "" {
		return def
	}
	i, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return i
}

func floatFromAny(v any, def float64) float64 {
	if v == nil {
		return def
	}
	s := strings.TrimSpace(fmt.Sprint(v))
	if s == "" {
		return def
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return def
	}
	return f
}

func boolFromAny(v any, def bool) bool {
	if v == nil {
		return def
	}
	s := strings.TrimSpace(strings.ToLower(fmt.Sprint(v)))
	switch s {
	case "1", "true", "t", "yes", "y", "on":
		return true
	case "0", "false", "f", "no", "n", "off":
		return false
	default:
		return def
	}
}
