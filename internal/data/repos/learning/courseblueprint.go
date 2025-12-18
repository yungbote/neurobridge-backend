package learning

import (
	"context"
	"github.com/google/uuid"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"gorm.io/gorm"
)

type CourseBlueprintRepo interface {
	Create(ctx context.Context, tx *gorm.DB, blueprints []*types.CourseBlueprint) ([]*types.CourseBlueprint, error)
	GetByIDs(ctx context.Context, tx *gorm.DB, blueprintIDs []uuid.UUID) ([]*types.CourseBlueprint, error)
	GetByMaterialSetIDs(ctx context.Context, tx *gorm.DB, setIDs []uuid.UUID) ([]*types.CourseBlueprint, error)
	GetByUserIDs(ctx context.Context, tx *gorm.DB, userIDs []uuid.UUID) ([]*types.CourseBlueprint, error)
	SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, blueprintIDs []uuid.UUID) error
	FullDeleteByIDs(ctx context.Context, tx *gorm.DB, blueprintIDs []uuid.UUID) error
}

type courseBlueprintRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewCourseBlueprintRepo(db *gorm.DB, baseLog *logger.Logger) CourseBlueprintRepo {
	repoLog := baseLog.With("repo", "CourseBlueprintRepo")
	return &courseBlueprintRepo{db: db, log: repoLog}
}

func (r *courseBlueprintRepo) Create(ctx context.Context, tx *gorm.DB, blueprints []*types.CourseBlueprint) ([]*types.CourseBlueprint, error) {
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}

	if len(blueprints) == 0 {
		return []*types.CourseBlueprint{}, nil
	}

	if err := transaction.WithContext(ctx).Create(&blueprints).Error; err != nil {
		return nil, err
	}
	return blueprints, nil
}

func (r *courseBlueprintRepo) GetByIDs(ctx context.Context, tx *gorm.DB, blueprintIDs []uuid.UUID) ([]*types.CourseBlueprint, error) {
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}

	var results []*types.CourseBlueprint
	if len(blueprintIDs) == 0 {
		return results, nil
	}

	if err := transaction.WithContext(ctx).
		Where("id IN ?", blueprintIDs).
		Find(&results).Error; err != nil {
		return nil, err
	}
	return results, nil
}

func (r *courseBlueprintRepo) GetByMaterialSetIDs(ctx context.Context, tx *gorm.DB, setIDs []uuid.UUID) ([]*types.CourseBlueprint, error) {
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}

	var results []*types.CourseBlueprint
	if len(setIDs) == 0 {
		return results, nil
	}

	if err := transaction.WithContext(ctx).
		Where("material_set_id IN ?", setIDs).
		Find(&results).Error; err != nil {
		return nil, err
	}
	return results, nil
}

func (r *courseBlueprintRepo) GetByUserIDs(ctx context.Context, tx *gorm.DB, userIDs []uuid.UUID) ([]*types.CourseBlueprint, error) {
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}

	var results []*types.CourseBlueprint
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

func (r *courseBlueprintRepo) SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, blueprintIDs []uuid.UUID) error {
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}

	if len(blueprintIDs) == 0 {
		return nil
	}

	if err := transaction.WithContext(ctx).
		Where("id IN ?", blueprintIDs).
		Delete(&types.CourseBlueprint{}).Error; err != nil {
		return err
	}
	return nil
}

func (r *courseBlueprintRepo) FullDeleteByIDs(ctx context.Context, tx *gorm.DB, blueprintIDs []uuid.UUID) error {
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}

	if len(blueprintIDs) == 0 {
		return nil
	}

	if err := transaction.WithContext(ctx).
		Unscoped().
		Where("id IN ?", blueprintIDs).
		Delete(&types.CourseBlueprint{}).Error; err != nil {
		return err
	}
	return nil
}
