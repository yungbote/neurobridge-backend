package learning

import (
	"errors"
	"time"

	"github.com/google/uuid"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"gorm.io/gorm"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
)

type LearningProfileRepo interface {
	Upsert(dbc dbctx.Context, row *types.LearningProfile) error
	GetByUserID(dbc dbctx.Context, userID uuid.UUID) (*types.LearningProfile, error)
}

type learningProfileRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewLearningProfileRepo(db *gorm.DB, baseLog *logger.Logger) LearningProfileRepo {
	return &learningProfileRepo{db: db, log: baseLog.With("repo", "LearningProfileRepo")}
}

func (r *learningProfileRepo) dbx(dbc dbctx.Context) *gorm.DB {
	if dbc.Tx != nil {
		return dbc.Tx
	}
	return r.db
}

func (r *learningProfileRepo) GetByUserID(dbc dbctx.Context, userID uuid.UUID) (*types.LearningProfile, error) {
	if userID == uuid.Nil {
		return nil, nil
	}
	var out types.LearningProfile
	err := r.dbx(dbc).WithContext(dbc.Ctx).
		Where("user_id = ?", userID).
		First(&out).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (r *learningProfileRepo) Upsert(dbc dbctx.Context, row *types.LearningProfile) error {
	if row == nil || row.UserID == uuid.Nil {
		return nil
	}
	now := time.Now().UTC()
	row.UpdatedAt = now
	if row.CreatedAt.IsZero() {
		row.CreatedAt = now
	}

	t := r.dbx(dbc).WithContext(dbc.Ctx)

	existing, err := r.GetByUserID(dbc, row.UserID)
	if err != nil {
		return err
	}
	if existing != nil && existing.ID != uuid.Nil {
		row.ID = existing.ID
		return t.Save(row).Error
	}
	return t.Create(row).Error
}
