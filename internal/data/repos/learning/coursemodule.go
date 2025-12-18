package learning

import (
	"context"
	"github.com/google/uuid"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"gorm.io/gorm"
)

type CourseModuleRepo interface {
	Create(ctx context.Context, tx *gorm.DB, modules []*types.CourseModule) ([]*types.CourseModule, error)
	GetByIDs(ctx context.Context, tx *gorm.DB, moduleIDs []uuid.UUID) ([]*types.CourseModule, error)
	GetByCourseIDs(ctx context.Context, tx *gorm.DB, courseIDs []uuid.UUID) ([]*types.CourseModule, error)
	SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, moduleIDs []uuid.UUID) error
	SoftDeleteByCourseIDs(ctx context.Context, tx *gorm.DB, courseIDs []uuid.UUID) error
	FullDeleteByIDs(ctx context.Context, tx *gorm.DB, moduleIDs []uuid.UUID) error
	FullDeleteByCourseIDs(ctx context.Context, tx *gorm.DB, courseIDs []uuid.UUID) error
}

type courseModuleRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewCourseModuleRepo(db *gorm.DB, baseLog *logger.Logger) CourseModuleRepo {
	repoLog := baseLog.With("repo", "CourseModuleRepo")
	return &courseModuleRepo{db: db, log: repoLog}
}

func (r *courseModuleRepo) Create(ctx context.Context, tx *gorm.DB, modules []*types.CourseModule) ([]*types.CourseModule, error) {
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}

	if len(modules) == 0 {
		return []*types.CourseModule{}, nil
	}

	if err := transaction.WithContext(ctx).Create(&modules).Error; err != nil {
		return nil, err
	}
	return modules, nil
}

func (r *courseModuleRepo) GetByIDs(ctx context.Context, tx *gorm.DB, moduleIDs []uuid.UUID) ([]*types.CourseModule, error) {
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}

	var results []*types.CourseModule
	if len(moduleIDs) == 0 {
		return results, nil
	}

	if err := transaction.WithContext(ctx).
		Where("id IN ?", moduleIDs).
		Find(&results).Error; err != nil {
		return nil, err
	}
	return results, nil
}

func (r *courseModuleRepo) GetByCourseIDs(ctx context.Context, tx *gorm.DB, courseIDs []uuid.UUID) ([]*types.CourseModule, error) {
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}

	var results []*types.CourseModule
	if len(courseIDs) == 0 {
		return results, nil
	}

	if err := transaction.WithContext(ctx).
		Where("course_id IN ?", courseIDs).
		Order("course_id, index ASC").
		Find(&results).Error; err != nil {
		return nil, err
	}
	return results, nil
}

func (r *courseModuleRepo) SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, moduleIDs []uuid.UUID) error {
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}

	if len(moduleIDs) == 0 {
		return nil
	}

	if err := transaction.WithContext(ctx).
		Where("id IN ?", moduleIDs).
		Delete(&types.CourseModule{}).Error; err != nil {
		return err
	}
	return nil
}

func (r *courseModuleRepo) SoftDeleteByCourseIDs(ctx context.Context, tx *gorm.DB, courseIDs []uuid.UUID) error {
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}

	if len(courseIDs) == 0 {
		return nil
	}

	if err := transaction.WithContext(ctx).
		Where("course_id IN ?", courseIDs).
		Delete(&types.CourseModule{}).Error; err != nil {
		return err
	}
	return nil
}

func (r *courseModuleRepo) FullDeleteByIDs(ctx context.Context, tx *gorm.DB, moduleIDs []uuid.UUID) error {
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}

	if len(moduleIDs) == 0 {
		return nil
	}

	if err := transaction.WithContext(ctx).
		Unscoped().
		Where("id IN ?", moduleIDs).
		Delete(&types.CourseModule{}).Error; err != nil {
		return err
	}
	return nil
}

func (r *courseModuleRepo) FullDeleteByCourseIDs(ctx context.Context, tx *gorm.DB, courseIDs []uuid.UUID) error {
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}

	if len(courseIDs) == 0 {
		return nil
	}

	if err := transaction.WithContext(ctx).
		Unscoped().
		Where("course_id IN ?", courseIDs).
		Delete(&types.CourseModule{}).Error; err != nil {
		return err
	}
	return nil
}
