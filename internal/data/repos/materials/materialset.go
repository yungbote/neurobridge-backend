package materials

import (
	"context"

	"github.com/google/uuid"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type MaterialSetRepo interface {
	Create(ctx context.Context, tx *gorm.DB, sets []*types.MaterialSet) ([]*types.MaterialSet, error)
	GetByIDs(ctx context.Context, tx *gorm.DB, setIDs []uuid.UUID) ([]*types.MaterialSet, error)
	GetByUserIDs(ctx context.Context, tx *gorm.DB, userIDs []uuid.UUID) ([]*types.MaterialSet, error)
	GetByStatus(ctx context.Context, tx *gorm.DB, statuses []string) ([]*types.MaterialSet, error)
	SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, setIDs []uuid.UUID) error
	FullDeleteByIDs(ctx context.Context, tx *gorm.DB, setIDs []uuid.UUID) error
}

type materialSetRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewMaterialSetRepo(db *gorm.DB, baseLog *logger.Logger) MaterialSetRepo {
	repoLog := baseLog.With("repo", "MaterialSetRepo")
	return &materialSetRepo{db: db, log: repoLog}
}

func (r *materialSetRepo) Create(ctx context.Context, tx *gorm.DB, sets []*types.MaterialSet) ([]*types.MaterialSet, error) {
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}

	if len(sets) == 0 {
		return []*types.MaterialSet{}, nil
	}

	if err := transaction.WithContext(ctx).Create(&sets).Error; err != nil {
		return nil, err
	}
	return sets, nil
}

func (r *materialSetRepo) GetByIDs(ctx context.Context, tx *gorm.DB, setIDs []uuid.UUID) ([]*types.MaterialSet, error) {
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}

	var results []*types.MaterialSet
	if len(setIDs) == 0 {
		return results, nil
	}

	if err := transaction.WithContext(ctx).
		Where("id IN ?", setIDs).
		Find(&results).Error; err != nil {
		return nil, err
	}
	return results, nil
}

func (r *materialSetRepo) GetByUserIDs(ctx context.Context, tx *gorm.DB, userIDs []uuid.UUID) ([]*types.MaterialSet, error) {
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}

	var results []*types.MaterialSet
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

func (r *materialSetRepo) GetByStatus(ctx context.Context, tx *gorm.DB, statuses []string) ([]*types.MaterialSet, error) {
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}

	var results []*types.MaterialSet
	if len(statuses) == 0 {
		return results, nil
	}

	if err := transaction.WithContext(ctx).
		Where("status IN ?", statuses).
		Find(&results).Error; err != nil {
		return nil, err
	}
	return results, nil
}

func (r *materialSetRepo) SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, setIDs []uuid.UUID) error {
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}

	if len(setIDs) == 0 {
		return nil
	}

	if err := transaction.WithContext(ctx).
		Where("id IN ?", setIDs).
		Delete(&types.MaterialSet{}).Error; err != nil {
		return err
	}
	return nil
}

func (r *materialSetRepo) FullDeleteByIDs(ctx context.Context, tx *gorm.DB, setIDs []uuid.UUID) error {
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}

	if len(setIDs) == 0 {
		return nil
	}

	if err := transaction.WithContext(ctx).
		Unscoped().
		Where("id IN ?", setIDs).
		Delete(&types.MaterialSet{}).Error; err != nil {
		return err
	}
	return nil
}
