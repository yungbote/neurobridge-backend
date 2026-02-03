package user

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type UserGazeEventRepo interface {
	CreateMany(dbc dbctx.Context, rows []*types.UserGazeEvent) error
	DeleteOlderThan(dbc dbctx.Context, userID uuid.UUID, cutoff string) error
}

type userGazeEventRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewUserGazeEventRepo(db *gorm.DB, baseLog *logger.Logger) UserGazeEventRepo {
	return &userGazeEventRepo{db: db, log: baseLog.With("repo", "UserGazeEventRepo")}
}

func (r *userGazeEventRepo) CreateMany(dbc dbctx.Context, rows []*types.UserGazeEvent) error {
	if len(rows) == 0 {
		return nil
	}
	return r.db.WithContext(dbc.Ctx).Create(rows).Error
}

func (r *userGazeEventRepo) DeleteOlderThan(dbc dbctx.Context, userID uuid.UUID, cutoff string) error {
	if userID == uuid.Nil {
		return nil
	}
	return r.db.WithContext(dbc.Ctx).
		Where("user_id = ? AND occurred_at < ?", userID, cutoff).
		Delete(&types.UserGazeEvent{}).Error
}
