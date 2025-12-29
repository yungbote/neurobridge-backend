package learning

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
)

type UserProgressionEventRepo interface {
	Create(dbc dbctx.Context, rows []*types.UserProgressionEvent) ([]*types.UserProgressionEvent, error)
	ListRecentByUser(dbc dbctx.Context, userID uuid.UUID, limit int) ([]*types.UserProgressionEvent, error)
	ListRecentAll(dbc dbctx.Context, limit int) ([]*types.UserProgressionEvent, error)
	ListByUserAndPathID(dbc dbctx.Context, userID uuid.UUID, pathID uuid.UUID, limit int) ([]*types.UserProgressionEvent, error)
}

type userProgressionEventRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewUserProgressionEventRepo(db *gorm.DB, baseLog *logger.Logger) UserProgressionEventRepo {
	return &userProgressionEventRepo{db: db, log: baseLog.With("repo", "UserProgressionEventRepo")}
}

func (r *userProgressionEventRepo) dbx(dbc dbctx.Context) *gorm.DB {
	if dbc.Tx != nil {
		return dbc.Tx
	}
	return r.db
}

func (r *userProgressionEventRepo) Create(dbc dbctx.Context, rows []*types.UserProgressionEvent) ([]*types.UserProgressionEvent, error) {
	t := r.dbx(dbc)
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
	if err := t.WithContext(dbc.Ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *userProgressionEventRepo) ListRecentByUser(dbc dbctx.Context, userID uuid.UUID, limit int) ([]*types.UserProgressionEvent, error) {
	t := r.dbx(dbc)
	out := []*types.UserProgressionEvent{}
	if userID == uuid.Nil {
		return out, nil
	}
	if limit <= 0 {
		limit = 500
	}
	if err := t.WithContext(dbc.Ctx).
		Where("user_id = ?", userID).
		Order("occurred_at DESC").
		Limit(limit).
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *userProgressionEventRepo) ListRecentAll(dbc dbctx.Context, limit int) ([]*types.UserProgressionEvent, error) {
	t := r.dbx(dbc)
	out := []*types.UserProgressionEvent{}
	if limit <= 0 {
		limit = 50000
	}
	if err := t.WithContext(dbc.Ctx).
		Order("occurred_at DESC").
		Limit(limit).
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *userProgressionEventRepo) ListByUserAndPathID(dbc dbctx.Context, userID uuid.UUID, pathID uuid.UUID, limit int) ([]*types.UserProgressionEvent, error) {
	t := r.dbx(dbc)
	out := []*types.UserProgressionEvent{}
	if userID == uuid.Nil || pathID == uuid.Nil {
		return out, nil
	}
	if limit <= 0 {
		limit = 5000
	}
	if limit > 50000 {
		limit = 50000
	}
	if err := t.WithContext(dbc.Ctx).
		Where("user_id = ? AND path_id = ?", userID, pathID).
		Order("occurred_at DESC").
		Limit(limit).
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}
