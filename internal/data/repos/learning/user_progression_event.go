package learning

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type UserProgressionEventRepo interface {
	Create(ctx context.Context, tx *gorm.DB, rows []*types.UserProgressionEvent) ([]*types.UserProgressionEvent, error)
	ListRecentByUser(ctx context.Context, tx *gorm.DB, userID uuid.UUID, limit int) ([]*types.UserProgressionEvent, error)
	ListRecentAll(ctx context.Context, tx *gorm.DB, limit int) ([]*types.UserProgressionEvent, error)
}

type userProgressionEventRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewUserProgressionEventRepo(db *gorm.DB, baseLog *logger.Logger) UserProgressionEventRepo {
	return &userProgressionEventRepo{db: db, log: baseLog.With("repo", "UserProgressionEventRepo")}
}

func (r *userProgressionEventRepo) dbx(tx *gorm.DB) *gorm.DB {
	if tx != nil {
		return tx
	}
	return r.db
}

func (r *userProgressionEventRepo) Create(ctx context.Context, tx *gorm.DB, rows []*types.UserProgressionEvent) ([]*types.UserProgressionEvent, error) {
	t := r.dbx(tx)
	if len(rows) == 0 {
		return []*types.UserProgressionEvent{}, nil
	}
	now := time.Now().UTC()
	for _, x := range rows {
		if x == nil {
			continue
		}
		if x.ID == uuid.Nil {
			x.ID = uuid.New()
		}
		if x.OccurredAt.IsZero() {
			x.OccurredAt = now
		}
		x.CreatedAt = now
		x.UpdatedAt = now
	}
	if err := t.WithContext(ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *userProgressionEventRepo) ListRecentByUser(ctx context.Context, tx *gorm.DB, userID uuid.UUID, limit int) ([]*types.UserProgressionEvent, error) {
	t := r.dbx(tx)
	out := []*types.UserProgressionEvent{}
	if userID == uuid.Nil {
		return out, nil
	}
	if limit <= 0 {
		limit = 500
	}
	if err := t.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("occurred_at DESC").
		Limit(limit).
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *userProgressionEventRepo) ListRecentAll(ctx context.Context, tx *gorm.DB, limit int) ([]*types.UserProgressionEvent, error) {
	t := r.dbx(tx)
	out := []*types.UserProgressionEvent{}
	if limit <= 0 {
		limit = 50000
	}
	if err := t.WithContext(ctx).
		Order("occurred_at DESC").
		Limit(limit).
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}










