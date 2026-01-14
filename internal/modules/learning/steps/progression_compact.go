package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type ProgressionCompactDeps struct {
	DB        *gorm.DB
	Log       *logger.Logger
	Events    repos.UserEventRepo
	Cursors   repos.UserEventCursorRepo
	Progress  repos.UserProgressionEventRepo
	Bootstrap services.LearningBuildBootstrapService
}

type ProgressionCompactInput struct {
	OwnerUserID   uuid.UUID
	MaterialSetID uuid.UUID
	SagaID        uuid.UUID
	PathID        uuid.UUID
}

type ProgressionCompactOutput struct {
	Processed int `json:"processed"`
}

func ProgressionCompact(ctx context.Context, deps ProgressionCompactDeps, in ProgressionCompactInput) (ProgressionCompactOutput, error) {
	out := ProgressionCompactOutput{}
	if deps.DB == nil || deps.Log == nil || deps.Events == nil || deps.Cursors == nil || deps.Progress == nil || deps.Bootstrap == nil {
		return out, fmt.Errorf("progression_compact: missing deps")
	}
	if in.OwnerUserID == uuid.Nil {
		return out, fmt.Errorf("progression_compact: missing owner_user_id")
	}
	if in.MaterialSetID == uuid.Nil {
		return out, fmt.Errorf("progression_compact: missing material_set_id")
	}

	// Contract: derive/ensure path_id.
	_, _ = resolvePathID(ctx, deps.Bootstrap, in.OwnerUserID, in.MaterialSetID, in.PathID)

	consumer := "progression_compact"

	var afterAt *time.Time
	var afterID *uuid.UUID
	if cur, err := deps.Cursors.Get(dbctx.Context{Ctx: ctx}, in.OwnerUserID, consumer); err == nil && cur != nil {
		afterAt = cur.LastCreatedAt
		afterID = cur.LastEventID
	}

	const pageSize = 500
	start := time.Now()

	for {
		events, err := deps.Events.ListAfterCursor(dbctx.Context{Ctx: ctx}, in.OwnerUserID, afterAt, afterID, pageSize)
		if err != nil {
			return out, err
		}
		if len(events) == 0 {
			break
		}

		// Transactionally: write progression facts + advance cursor.
		if err := deps.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			dbc := dbctx.Context{Ctx: ctx, Tx: tx}
			rows := make([]*types.UserProgressionEvent, 0, len(events))
			for _, ev := range events {
				if ev == nil || ev.ID == uuid.Nil {
					continue
				}
				out.Processed++

				var d map[string]any
				if len(ev.Data) > 0 && string(ev.Data) != "null" {
					_ = json.Unmarshal(ev.Data, &d)
				}

				conceptIDs := extractConceptIDs(ev, d)
				conceptJSON := datatypes.JSON(mustJSON(conceptIDs))

				pe := &types.UserProgressionEvent{
					ID:           uuid.New(),
					UserID:       ev.UserID,
					OccurredAt:   nonZeroTime(ev.OccurredAt),
					PathID:       ev.PathID,
					ActivityID:   ev.ActivityID,
					ConceptIDs:   conceptJSON,
					ActivityKind: strings.TrimSpace(stringFromAny(d["activity_kind"])),
					Variant:      strings.TrimSpace(ev.ActivityVariant),
					Completed:    strings.TrimSpace(ev.Type) == types.EventActivityCompleted,
					Score:        floatFromAny(d["score"], 0),
					DwellMS:      intFromAny(d["dwell_ms"], 0),
					Attempts:     intFromAny(d["attempts"], 0),
					Metadata:     datatypes.JSON(mustJSON(map[string]any{"event_type": ev.Type, "event_id": ev.ID.String()})),
					CreatedAt:    time.Now().UTC(),
					UpdatedAt:    time.Now().UTC(),
				}
				rows = append(rows, pe)

				// Cursor advance (tie-safe: created_at + id)
				t := ev.CreatedAt
				id := ev.ID
				afterAt = &t
				afterID = &id
			}

			if len(rows) > 0 {
				if _, err := deps.Progress.Create(dbc, rows); err != nil {
					return err
				}
			}

			// Persist cursor in same tx (idempotency on retry).
			if afterAt != nil && afterID != nil {
				curRow := &types.UserEventCursor{
					ID:            uuid.New(),
					UserID:        in.OwnerUserID,
					Consumer:      consumer,
					LastCreatedAt: afterAt,
					LastEventID:   afterID,
					UpdatedAt:     time.Now().UTC(),
				}
				if err := deps.Cursors.Upsert(dbc, curRow); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return out, err
		}

		// Budget guard.
		if time.Since(start) > 20*time.Second || out.Processed >= 4000 {
			break
		}
	}

	return out, nil
}

func nonZeroTime(t time.Time) time.Time {
	if t.IsZero() {
		return time.Now().UTC()
	}
	return t
}

func extractConceptIDs(ev *types.UserEvent, d map[string]any) []string {
	ids := []string{}
	if ev != nil && ev.ConceptID != nil && *ev.ConceptID != uuid.Nil {
		ids = append(ids, ev.ConceptID.String())
	}
	if d != nil {
		ids = append(ids, stringSliceFromAny(d["concept_ids"])...)
	}
	return dedupeStrings(ids)
}
