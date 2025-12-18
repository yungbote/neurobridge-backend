package learning

import (
	"context"
	"github.com/google/uuid"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"gorm.io/gorm"
)

type LearningProfileRepo interface {
	Create(ctx context.Context, tx *gorm.DB, profiles []*types.LearningProfile) ([]*types.LearningProfile, error)
	GetByIDs(ctx context.Context, tx *gorm.DB, profileIDs []uuid.UUID) ([]*types.LearningProfile, error)
	GetByUserIDs(ctx context.Context, tx *gorm.DB, userIDs []uuid.UUID) ([]*types.LearningProfile, error)
	Update(ctx context.Context, tx *gorm.DB, profile *types.LearningProfile) error
	SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, profileIDs []uuid.UUID) error
	FullDeleteByIDs(ctx context.Context, tx *gorm.DB, profileIDs []uuid.UUID) error
}

type learningProfileRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewLearningProfileRepo(db *gorm.DB, baseLog *logger.Logger) LearningProfileRepo {
	repoLog := baseLog.With("repo", "LearningProfileRepo")
	return &learningProfileRepo{db: db, log: repoLog}
}

func (r *learningProfileRepo) Create(ctx context.Context, tx *gorm.DB, profiles []*types.LearningProfile) ([]*types.LearningProfile, error) {
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}

	if len(profiles) == 0 {
		return []*types.LearningProfile{}, nil
	}

	if err := transaction.WithContext(ctx).Create(&profiles).Error; err != nil {
		return nil, err
	}
	return profiles, nil
}

func (r *learningProfileRepo) GetByIDs(ctx context.Context, tx *gorm.DB, profileIDs []uuid.UUID) ([]*types.LearningProfile, error) {
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}

	var results []*types.LearningProfile
	if len(profileIDs) == 0 {
		return results, nil
	}

	if err := transaction.WithContext(ctx).
		Where("id IN ?", profileIDs).
		Find(&results).Error; err != nil {
		return nil, err
	}
	return results, nil
}

func (r *learningProfileRepo) GetByUserIDs(ctx context.Context, tx *gorm.DB, userIDs []uuid.UUID) ([]*types.LearningProfile, error) {
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}

	var results []*types.LearningProfile
	if len(userIDs) == 0 {
		return results, nil
	}

	if err := transaction.WithContext(ctx).
		Where("user_id IN ?", userIDs).
		Find(&results).Error; err != nil {
		return nil, err
	}
	return results, nil
}

func (r *learningProfileRepo) Update(ctx context.Context, tx *gorm.DB, profile *types.LearningProfile) error {
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}

	if profile == nil {
		return nil
	}

	if err := transaction.WithContext(ctx).Save(profile).Error; err != nil {
		return err
	}
	return nil
}

func (r *learningProfileRepo) SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, profileIDs []uuid.UUID) error {
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}

	if len(profileIDs) == 0 {
		return nil
	}

	if err := transaction.WithContext(ctx).
		Where("id IN ?", profileIDs).
		Delete(&types.LearningProfile{}).Error; err != nil {
		return err
	}
	return nil
}

func (r *learningProfileRepo) FullDeleteByIDs(ctx context.Context, tx *gorm.DB, profileIDs []uuid.UUID) error {
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}

	if len(profileIDs) == 0 {
		return nil
	}

	if err := transaction.WithContext(ctx).
		Unscoped().
		Where("id IN ?", profileIDs).
		Delete(&types.LearningProfile{}).Error; err != nil {
		return err
	}
	return nil
}
