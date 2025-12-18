package user_model_update

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	jobrt "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
)

func (p *Pipeline) Run(ctx *jobrt.Context) error {
	if ctx == nil || ctx.Job == nil {
		return nil
	}

	userID := ctx.Job.OwnerUserID
	if userID == uuid.Nil {
		ctx.Fail("validate", fmt.Errorf("missing owner_user_id"))
		return nil
	}

	consumer := "user_model_update"

	// Load cursor (if missing, start from beginning)
	var afterAt *time.Time
	var afterID *uuid.UUID

	cur, err := p.cursors.Get(ctx.Ctx, nil, userID, consumer)
	if err == nil && cur != nil {
		afterAt = cur.LastCreatedAt
		afterID = cur.LastEventID
	}

	const pageSize = 500
	processed := 0
	start := time.Now()

	ctx.Progress("scan", 1, "Scanning user events")

	for {
		events, err := p.events.ListAfterCursor(ctx.Ctx, nil, userID, afterAt, afterID, pageSize)
		if err != nil {
			ctx.Fail("scan", err)
			return nil
		}
		if len(events) == 0 {
			break
		}

		for _, ev := range events {
			if ev == nil {
				continue
			}
			processed++

			// Parse event data once
			var d map[string]any
			if len(ev.Data) > 0 {
				_ = json.Unmarshal(ev.Data, &d)
			}

			// ---- Concept mastery updates (question_answered) ----
			if strings.TrimSpace(ev.Type) == types.EventQuestionAnswered {
				_ = p.applyQuestionAnswered(ctx.Ctx, nil, userID, ev, d)
			}

			// ---- Style preference updates (EMA) ----
			// Expect frontend event:
			// type="style_feedback"
			// data: { "modality":"diagram", "variant":"flowchart", "reward":0.7, "binary":true|false, "concept_id":"..." }
			if strings.TrimSpace(ev.Type) == "style_feedback" && p.stylePrefs != nil {
				modality := strings.TrimSpace(fmt.Sprint(d["modality"]))
				variant := strings.TrimSpace(fmt.Sprint(d["variant"]))
				reward := floatFromAny(d["reward"], 0)

				var conceptID *uuid.UUID
				if s := strings.TrimSpace(fmt.Sprint(d["concept_id"])); s != "" {
					if id, e := uuid.Parse(s); e == nil && id != uuid.Nil {
						conceptID = &id
					}
				}

				var binary *bool
				if bv, ok := d["binary"].(bool); ok {
					binary = &bv
				}

				if modality != "" && variant != "" {
					_ = p.stylePrefs.UpsertEMA(ctx.Ctx, nil, userID, conceptID, modality, variant, reward, binary)
				}
			}

			// Cursor advance (tie-safe cursor = created_at + id)
			t := ev.CreatedAt
			id := ev.ID
			afterAt = &t
			afterID = &id

			// Lightweight progress updates
			if processed%500 == 0 {
				ctx.Progress("scan", 25, fmt.Sprintf("Processed %d events", processed))
			}
		}

		// Budget guard: don’t monopolize worker for huge backlogs
		if time.Since(start) > 20*time.Second || processed >= 4000 {
			break
		}
	}

	// Persist cursor
	if afterAt != nil && afterID != nil {
		curRow := &types.UserEventCursor{
			ID:            uuid.New(),
			UserID:        userID,
			Consumer:      consumer,
			LastCreatedAt: afterAt,
			LastEventID:   afterID,
			UpdatedAt:     time.Now(),
		}
		_ = p.cursors.Upsert(ctx.Ctx, nil, curRow)
	}

	ctx.Succeed("done", map[string]any{"processed": processed})
	return nil
}
