package learning

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type UserCompletedUnitRepo interface {
	Get(ctx context.Context, tx *gorm.DB, userID uuid.UUID, chainKey string) (*types.UserCompletedUnit, error)
	Upsert(ctx context.Context, tx *gorm.DB, row *types.UserCompletedUnit) error
	ListByUser(ctx context.Context, tx *gorm.DB, userID uuid.UUID, limit int) ([]*types.UserCompletedUnit, error)
}

type userCompletedUnitRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewUserCompletedUnitRepo(db *gorm.DB, baseLog *logger.Logger) UserCompletedUnitRepo {
	return &userCompletedUnitRepo{db: db, log: baseLog.With("repo", "UserCompletedUnitRepo")}
}

func (r *userCompletedUnitRepo) Get(ctx context.Context, tx *gorm.DB, userID uuid.UUID, chainKey string) (*types.UserCompletedUnit, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	if userID == uuid.Nil || chainKey == "" {
		return nil, nil
	}
	var row types.UserCompletedUnit
	err := t.WithContext(ctx).
		Where("user_id = ? AND chain_key = ?", userID, chainKey).
		Limit(1).
		Find(&row).Error
	if err != nil {
		return nil, err
	}
	if row.ID == uuid.Nil {
		return nil, nil
	}
	return &row, nil
}

func (r *userCompletedUnitRepo) ListByUser(ctx context.Context, tx *gorm.DB, userID uuid.UUID, limit int) ([]*types.UserCompletedUnit, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.UserCompletedUnit
	if userID == uuid.Nil {
		return out, nil
	}
	if limit <= 0 {
		limit = 200
	}
	if limit > 2000 {
		limit = 2000
	}
	if err := t.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("updated_at DESC").
		Limit(limit).
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *userCompletedUnitRepo) Upsert(ctx context.Context, tx *gorm.DB, row *types.UserCompletedUnit) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if row == nil || row.UserID == uuid.Nil || row.ChainKey == "" {
		return nil
	}
	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	row.UpdatedAt = time.Now().UTC()

	return t.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "user_id"}, {Name: "chain_key"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"completed_at",
				"completion_confidence",
				"mastery_at",
				"avg_score",
				"total_dwell_ms",
				"attempts",
				"metadata",
				"updated_at",
			}),
		}).
		Create(row).Error
}
