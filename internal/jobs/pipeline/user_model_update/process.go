package user_model_update

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"github.com/yungbote/neurobridge-backend/internal/types"
)

func (p *UserModelUpdatePipeline) applyEvents(ctx context.Context, tx *gorm.DB, userID uuid.UUID, events []*types.UserEvent) error {
	if userID == uuid.Nil || len(events) == 0 {
		return nil
	}
	for _, ev := range events {
		if ev == nil {
			continue
		}
		// parse data JSON lazily
		data := map[string]any{}
		if len(ev.Data) > 0 {
			_ = json.Unmarshal(ev.Data, &data)
		}
		// concept_ids can live in Data or via ev.ConceptID shortcut
		conceptIDs := extractUUIDsFromAny(data["concept_ids"])
		if ev.ConceptID != nil && *ev.ConceptID != uuid.Nil && len(conceptIDs) == 0 {
			conceptIDs = []uuid.UUID{*ev.ConceptID}
		}
		switch ev.Type {
		case "question_answered":
			// data: {is_correct: bool, latency_ms?: number, confidence?: number}
			isCorrect := boolFromAny(data["is_correct"], false)
			lat := intFromAny(data["latency_ms"], 0)
			for _, cid := range conceptIDs {
				if cid == uuid.Nil {
					continue
				}
				// naive mastery update
				// correct => move mastery toward 1; wrong => decay
				prev, _ := p.conceptStateRepo.Get(ctx, tx, userID, cid)
				m := 0.0
				c := 0.0
				if prev != nil {
					m = clamp01(prev.Mastery)
					c = clamp01(prev.Confidence)
				}
				alpha := 0.06
				if lat > 0 && lat > 12000 {
					alpha = 0.04 // slow answer => smaller reward
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

				seenAt := ev.OccurredAt
				_ = p.conceptStateRepo.UpsertDelta(ctx, tx, userID, cid, m, c, &seenAt)
			}
		case "activity_completed":
			// reward style preference if modality exists
			mod := pickModality(ev.Modality, data)
			if mod != "" {
				// optional concept-specific
				var conceptID *uuid.UUID
				if len(conceptIDs) == 1 {
					tmp := conceptIDs[0]
					conceptID = &tmp
				}
				// simple running score
				scoreDelta := 1.0
				_ = p.bumpStyle(ctx, tx, userID, conceptID, mod, scoreDelta)
			}
		case "activity_abandoned":
			mod := pickModality(ev.Modality, data)
			if mod != "" {
				var conceptID *uuid.UUID
				if len(conceptIDs) == 1 {
					tmp := conceptIDs[0]
					conceptID = &tmp
				}
				_ = p.bumpStyle(ctx, tx, userID, conceptID, mod, -0.5)
			}
		case "feedback_loved_diagram", "feedback_thumbs_up":
			mod := pickModality(ev.Modality, data)
			if mod != "" {
				var conceptID *uuid.UUID
				if len(conceptIDs) == 1 {
					tmp := conceptIDs[0]
					conceptID = &tmp
				}
				_ = p.bumpStyle(ctx, tx, userID, conceptID, mod, 1.5)
			}
		case "feedback_confusing", "feedback_thumbs_down":
			mod := pickModality(ev.Modality, data)
			if mod != "" {
				var conceptID *uuid.UUID
				if len(conceptIDs) == 1 {
					tmp := conceptIDs[0]
					conceptID = &tmp
				}
				_ = p.bumpStyle(ctx, tx, userID, conceptID, mod, -1.0)
			}
		}
	}

	return nil
}

func (p *UserModelUpdatePipeline) bumpStyle(ctx context.Context, tx *gorm.DB, userID uuid.UUID, conceptID *uuid.UUID, modality string, delta float64) error {
	// We don’t have a “get” method right now; simplest production approach:
	// store delta into score and increment n in a single upsert. For v1, just accumulate.
	// You can upgrade this to EMA/bandit later.
	nowScore := delta
	nowN := 1
	return p.stylePrefRepo.UpsertScore(ctx, tx, userID, conceptID, modality, nowScore, nowN)
}

// ---------- helpers ----------

func pickModality(evMod string, data map[string]any) string {
	m := strings.TrimSpace(evMod)
	if m != "" {
		return m
	}
	if data == nil {
		return ""
	}
	if v, ok := data["modality"]; ok {
		return strings.TrimSpace(fmt.Sprint(v))
	}
	return ""
}

func extractUUIDsFromAny(v any) []uuid.UUID {
	arr, ok := v.([]any)
	if !ok {
		// could already be []string
		if ss, ok2 := v.([]string); ok2 {
			out := make([]uuid.UUID, 0, len(ss))
			for _, s := range ss {
				id, err := uuid.Parse(strings.TrimSpace(s))
				if err == nil {
					out = append(out, id)
				}
			}
			return out
		}
		return nil
	}
	out := make([]uuid.UUID, 0, len(arr))
	for _, x := range arr {
		id, err := uuid.Parse(strings.TrimSpace(fmt.Sprint(x)))
		if err == nil {
			out = append(out, id)
		}
	}
	return out
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
	case float64:
		return int(t)
	case json.Number:
		i, _ := t.Int64()
		return int(i)
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










