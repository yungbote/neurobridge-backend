package rollback

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type ReconcileDeps struct {
	DB     *gorm.DB
	Log    *logger.Logger
	JobSvc services.JobService
}

type ReconcileInput struct {
	UserIDs   []uuid.UUID
	BatchSize int
	MaxUsers  int
	Trigger   string
}

type ReconcileOutput struct {
	UsersScanned int
	UsersQueued  int
	RowsCleared  map[string]int
	StartedAt    time.Time
	FinishedAt   time.Time
}

func ReconcileUserState(ctx context.Context, deps ReconcileDeps, input ReconcileInput) (ReconcileOutput, error) {
	out := ReconcileOutput{
		RowsCleared: map[string]int{},
		StartedAt:   time.Now().UTC(),
	}
	if deps.DB == nil {
		return out, fmt.Errorf("rollback reconcile: missing db")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if input.BatchSize <= 0 {
		input.BatchSize = 200
	}
	if input.BatchSize > 2000 {
		input.BatchSize = 2000
	}
	if strings.TrimSpace(input.Trigger) == "" {
		input.Trigger = "rollback_reconcile"
	}

	userIDs := input.UserIDs
	if len(userIDs) == 0 {
		userIDs = listUserIDs(ctx, deps.DB, input.BatchSize, input.MaxUsers)
	}
	for _, userID := range userIDs {
		if userID == uuid.Nil {
			continue
		}
		out.UsersScanned++
		rows, err := resetUserState(ctx, deps.DB, userID)
		if err != nil {
			if deps.Log != nil {
				deps.Log.Warn("rollback reconcile: reset failed", "user_id", userID.String(), "error", err.Error())
			}
			continue
		}
		for k, v := range rows {
			out.RowsCleared[k] += v
		}
		if deps.JobSvc != nil {
			if _, queued, err := deps.JobSvc.EnqueueDebouncedUserModelUpdate(dbctx.Context{Ctx: ctx}, userID); err == nil && queued {
				out.UsersQueued++
			}
		}
	}
	out.FinishedAt = time.Now().UTC()
	return out, nil
}

func listUserIDs(ctx context.Context, db *gorm.DB, batchSize int, maxUsers int) []uuid.UUID {
	if db == nil {
		return nil
	}
	if batchSize <= 0 {
		batchSize = 200
	}
	out := make([]uuid.UUID, 0)
	var last uuid.UUID
	for {
		ids := []uuid.UUID{}
		q := db.WithContext(ctx).Model(&types.User{}).Select("id").Order("id asc").Limit(batchSize)
		if last != uuid.Nil {
			q = q.Where("id > ?", last)
		}
		if err := q.Pluck("id", &ids).Error; err != nil {
			break
		}
		if len(ids) == 0 {
			break
		}
		for _, id := range ids {
			out = append(out, id)
			last = id
			if maxUsers > 0 && len(out) >= maxUsers {
				return out
			}
		}
	}
	return out
}

func resetUserState(ctx context.Context, db *gorm.DB, userID uuid.UUID) (map[string]int, error) {
	if db == nil || userID == uuid.Nil {
		return nil, nil
	}
	rows := map[string]int{}
	return rows, db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		rows["user_concept_state"] = deleteByUser(ctx, tx, &types.UserConceptState{}, userID)
		rows["user_concept_edge_stat"] = deleteByUser(ctx, tx, &types.UserConceptEdgeStat{}, userID)
		rows["user_concept_model"] = deleteByUser(ctx, tx, &types.UserConceptModel{}, userID)
		rows["user_concept_evidence"] = deleteByUser(ctx, tx, &types.UserConceptEvidence{}, userID)
		rows["user_concept_calibration"] = deleteByUser(ctx, tx, &types.UserConceptCalibration{}, userID)
		rows["user_testlet_state"] = deleteByUser(ctx, tx, &types.UserTestletState{}, userID)
		rows["user_skill_state"] = deleteByUser(ctx, tx, &types.UserSkillState{}, userID)
		rows["user_misconception_instance"] = deleteByUser(ctx, tx, &types.UserMisconceptionInstance{}, userID)
		rows["misconception_resolution_state"] = deleteByUser(ctx, tx, &types.MisconceptionResolutionState{}, userID)
		if err := tx.WithContext(ctx).
			Where("user_id = ? AND consumer = ?", userID, "user_model_update").
			Delete(&types.UserEventCursor{}).Error; err != nil {
			return err
		}
		return nil
	})
}

func deleteByUser(ctx context.Context, tx *gorm.DB, model any, userID uuid.UUID) int {
	if tx == nil {
		return 0
	}
	res := tx.WithContext(ctx).
		Unscoped().
		Where("user_id = ?", userID).
		Delete(model)
	if res.Error != nil {
		return 0
	}
	return int(res.RowsAffected)
}
