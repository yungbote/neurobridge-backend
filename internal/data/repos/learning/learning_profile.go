package learning

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"gorm.io/gorm"
)

type LearningProfileRepo interface {
	Upsert(ctx context.Context, tx *gorm.DB, row *types.LearningProfile) error
	GetByUserID(ctx context.Context, tx *gorm.DB, userID uuid.UUID) (*types.LearningProfile, error)
}

type learningProfileRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewLearningProfileRepo(db *gorm.DB, baseLog *logger.Logger) LearningProfileRepo {
	return &learningProfileRepo{db: db, log: baseLog.With("repo", "LearningProfileRepo")}
}

func (r *learningProfileRepo) dbx(tx *gorm.DB) *gorm.DB {
	if tx != nil {
		return tx
	}
	return r.db
}

func (r *learningProfileRepo) GetByUserID(ctx context.Context, tx *gorm.DB, userID uuid.UUID) (*types.LearningProfile, error) {
	if userID == uuid.Nil {
		return nil, nil
	}
	var out types.LearningProfile
	err := r.dbx(tx).WithContext(ctx).
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

func (r *learningProfileRepo) Upsert(ctx context.Context, tx *gorm.DB, row *types.LearningProfile) error {
	if row == nil || row.UserID == uuid.Nil {
		return nil
	}
	now := time.Now().UTC()
	row.UpdatedAt = now
	if row.CreatedAt.IsZero() {
		row.CreatedAt = now
	}

	t := r.dbx(tx).WithContext(ctx)

	existing, err := r.GetByUserID(ctx, tx, row.UserID)
	if err != nil {
		return err
	}
	if existing != nil && existing.ID != uuid.Nil {
		row.ID = existing.ID
		return t.Save(row).Error
	}
	return t.Create(row).Error
}
