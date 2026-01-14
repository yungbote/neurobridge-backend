package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type VariantStatsRefreshDeps struct {
	DB        *gorm.DB
	Log       *logger.Logger
	Events    repos.UserEventRepo
	Cursors   repos.UserEventCursorRepo
	Variants  repos.ActivityVariantRepo
	Stats     repos.ActivityVariantStatRepo
	Bootstrap services.LearningBuildBootstrapService
}

type VariantStatsRefreshInput struct {
	OwnerUserID   uuid.UUID
	MaterialSetID uuid.UUID
	SagaID        uuid.UUID
	PathID        uuid.UUID
}

type VariantStatsRefreshOutput struct {
	Processed int `json:"processed"`
	Updated   int `json:"updated"`
}

func VariantStatsRefresh(ctx context.Context, deps VariantStatsRefreshDeps, in VariantStatsRefreshInput) (VariantStatsRefreshOutput, error) {
	out := VariantStatsRefreshOutput{}
	if deps.DB == nil || deps.Log == nil || deps.Events == nil || deps.Cursors == nil || deps.Variants == nil || deps.Stats == nil || deps.Bootstrap == nil {
		return out, fmt.Errorf("variant_stats_refresh: missing deps")
	}
	if in.OwnerUserID == uuid.Nil {
		return out, fmt.Errorf("variant_stats_refresh: missing owner_user_id")
	}
	if in.MaterialSetID == uuid.Nil {
		return out, fmt.Errorf("variant_stats_refresh: missing material_set_id")
	}

	// Contract: derive/ensure path_id.
	_, _ = resolvePathID(ctx, deps.Bootstrap, in.OwnerUserID, in.MaterialSetID, in.PathID)

	consumer := "variant_stats_refresh"

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

		if err := deps.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			dbc := dbctx.Context{Ctx: ctx, Tx: tx}
			cache := map[uuid.UUID]*types.ActivityVariantStat{}

			for _, ev := range events {
				if ev == nil || ev.ID == uuid.Nil {
					continue
				}
				out.Processed++

				if ev.ActivityID == nil || *ev.ActivityID == uuid.Nil {
					continue
				}
				variant := strings.TrimSpace(ev.ActivityVariant)
				if variant == "" {
					variant = "default"
				}
				vrow, _ := deps.Variants.GetByActivityAndVariant(dbc, *ev.ActivityID, variant)
				if vrow == nil || vrow.ID == uuid.Nil {
					continue
				}

				stat := cache[vrow.ID]
				if stat == nil {
					rows, _ := deps.Stats.GetByVariantIDs(dbc, []uuid.UUID{vrow.ID})
					if len(rows) > 0 {
						stat = rows[0]
					}
					if stat == nil {
						stat = &types.ActivityVariantStat{ActivityVariantID: vrow.ID}
					}
					cache[vrow.ID] = stat
				}

				var d map[string]any
				if len(ev.Data) > 0 && string(ev.Data) != "null" {
					_ = json.Unmarshal(ev.Data, &d)
				}

				observedAt := ev.OccurredAt
				if observedAt.IsZero() {
					observedAt = time.Now().UTC()
				}

				switch strings.TrimSpace(ev.Type) {
				case types.EventActivityStarted:
					stat.Starts += 1
					stat.LastObservedAt = &observedAt
				case types.EventActivityCompleted:
					stat.Completions += 1
					stat.LastObservedAt = &observedAt
					// Update avg score / dwell using simple incremental mean over completions.
					n := float64(stat.Completions)
					score := floatFromAny(d["score"], 0)
					dwell := intFromAny(d["dwell_ms"], 0)
					if n > 0 {
						stat.AvgScore = ((stat.AvgScore * (n - 1)) + score) / n
						stat.AvgDwellMS = int(((float64(stat.AvgDwellMS) * (n - 1)) + float64(dwell)) / n)
					}
				case types.EventFeedbackThumbsUp:
					stat.ThumbsUp += 1
					stat.LastObservedAt = &observedAt
				case types.EventFeedbackThumbsDown:
					stat.ThumbsDown += 1
					stat.LastObservedAt = &observedAt
				default:
					// ignore other event types
				}

				if err := deps.Stats.Upsert(dbc, stat); err == nil {
					out.Updated++
				}

				// Cursor advance (tie-safe)
				t := ev.CreatedAt
				id := ev.ID
				afterAt = &t
				afterID = &id
			}

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

		if time.Since(start) > 20*time.Second || out.Processed >= 4000 {
			break
		}
	}

	return out, nil
}
