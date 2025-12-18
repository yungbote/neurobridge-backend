package learning

import (
	"context"
	"github.com/google/uuid"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"gorm.io/gorm"
)

type LessonRepo interface {
	Create(ctx context.Context, tx *gorm.DB, lessons []*types.Lesson) ([]*types.Lesson, error)
	GetByIDs(ctx context.Context, tx *gorm.DB, lessonIDs []uuid.UUID) ([]*types.Lesson, error)
	GetByModuleIDs(ctx context.Context, tx *gorm.DB, moduleIDs []uuid.UUID) ([]*types.Lesson, error)
	SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, lessonIDs []uuid.UUID) error
	SoftDeleteByModuleIDs(ctx context.Context, tx *gorm.DB, moduleIDs []uuid.UUID) error
	FullDeleteByIDs(ctx context.Context, tx *gorm.DB, lessonIDs []uuid.UUID) error
	FullDeleteByModuleIDs(ctx context.Context, tx *gorm.DB, moduleIDs []uuid.UUID) error
}

type lessonRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewLessonRepo(db *gorm.DB, baseLog *logger.Logger) LessonRepo {
	repoLog := baseLog.With("repo", "LessonRepo")
	return &lessonRepo{db: db, log: repoLog}
}

func (r *lessonRepo) Create(ctx context.Context, tx *gorm.DB, lessons []*types.Lesson) ([]*types.Lesson, error) {
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}

	if len(lessons) == 0 {
		return []*types.Lesson{}, nil
	}

	if err := transaction.WithContext(ctx).Create(&lessons).Error; err != nil {
		return nil, err
	}
	return lessons, nil
}

func (r *lessonRepo) GetByIDs(ctx context.Context, tx *gorm.DB, lessonIDs []uuid.UUID) ([]*types.Lesson, error) {
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}

	var results []*types.Lesson
	if len(lessonIDs) == 0 {
		return results, nil
	}

	if err := transaction.WithContext(ctx).
		Where("id IN ?", lessonIDs).
		Find(&results).Error; err != nil {
		return nil, err
	}
	return results, nil
}

func (r *lessonRepo) GetByModuleIDs(ctx context.Context, tx *gorm.DB, moduleIDs []uuid.UUID) ([]*types.Lesson, error) {
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}

	var results []*types.Lesson
	if len(moduleIDs) == 0 {
		return results, nil
	}

	if err := transaction.WithContext(ctx).
		Where("module_id IN ?", moduleIDs).
		Order("module_id, index ASC").
		Find(&results).Error; err != nil {
		return nil, err
	}
	return results, nil
}

func (r *lessonRepo) SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, lessonIDs []uuid.UUID) error {
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}

	if len(lessonIDs) == 0 {
		return nil
	}

	if err := transaction.WithContext(ctx).
		Where("id IN ?", lessonIDs).
		Delete(&types.Lesson{}).Error; err != nil {
		return err
	}
	return nil
}

func (r *lessonRepo) SoftDeleteByModuleIDs(ctx context.Context, tx *gorm.DB, moduleIDs []uuid.UUID) error {
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}

	if len(moduleIDs) == 0 {
		return nil
	}

	if err := transaction.WithContext(ctx).
		Where("module_id IN ?", moduleIDs).
		Delete(&types.Lesson{}).Error; err != nil {
		return err
	}
	return nil
}

func (r *lessonRepo) FullDeleteByIDs(ctx context.Context, tx *gorm.DB, lessonIDs []uuid.UUID) error {
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}

	if len(lessonIDs) == 0 {
		return nil
	}

	if err := transaction.WithContext(ctx).
		Unscoped().
		Where("id IN ?", lessonIDs).
		Delete(&types.Lesson{}).Error; err != nil {
		return err
	}
	return nil
}

func (r *lessonRepo) FullDeleteByModuleIDs(ctx context.Context, tx *gorm.DB, moduleIDs []uuid.UUID) error {
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}

	if len(moduleIDs) == 0 {
		return nil
	}

	if err := transaction.WithContext(ctx).
		Unscoped().
		Where("module_id IN ?", moduleIDs).
		Delete(&types.Lesson{}).Error; err != nil {
		return err
	}
	return nil
}
