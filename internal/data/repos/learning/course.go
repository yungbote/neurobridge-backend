package learning

import (
	"context"
	"github.com/google/uuid"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"gorm.io/gorm"
)

type CourseRepo interface {
	Create(ctx context.Context, tx *gorm.DB, courses []*types.Course) ([]*types.Course, error)
	GetByIDs(ctx context.Context, tx *gorm.DB, courseIDs []uuid.UUID) ([]*types.Course, error)
	GetByUserIDs(ctx context.Context, tx *gorm.DB, userIDs []uuid.UUID) ([]*types.Course, error)
	GetByMaterialSetIDs(ctx context.Context, tx *gorm.DB, setIDs []uuid.UUID) ([]*types.Course, error)
	SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, courseIDs []uuid.UUID) error
	FullDeleteByIDs(ctx context.Context, tx *gorm.DB, courseIDs []uuid.UUID) error
}

type courseRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewCourseRepo(db *gorm.DB, baseLog *logger.Logger) CourseRepo {
	repoLog := baseLog.With("repo", "CourseRepo")
	return &courseRepo{db: db, log: repoLog}
}

func (r *courseRepo) Create(ctx context.Context, tx *gorm.DB, courses []*types.Course) ([]*types.Course, error) {
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}

	if len(courses) == 0 {
		return []*types.Course{}, nil
	}

	if err := transaction.WithContext(ctx).Create(&courses).Error; err != nil {
		return nil, err
	}
	return courses, nil
}

func (r *courseRepo) GetByIDs(ctx context.Context, tx *gorm.DB, courseIDs []uuid.UUID) ([]*types.Course, error) {
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}

	var results []*types.Course
	if len(courseIDs) == 0 {
		return results, nil
	}

	if err := transaction.WithContext(ctx).
		Where("id IN ?", courseIDs).
		Find(&results).Error; err != nil {
		return nil, err
	}
	return results, nil
}

func (r *courseRepo) GetByUserIDs(ctx context.Context, tx *gorm.DB, userIDs []uuid.UUID) ([]*types.Course, error) {
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}

	var results []*types.Course
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

func (r *courseRepo) GetByMaterialSetIDs(ctx context.Context, tx *gorm.DB, setIDs []uuid.UUID) ([]*types.Course, error) {
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}

	var results []*types.Course
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

func (r *courseRepo) SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, courseIDs []uuid.UUID) error {
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}

	if len(courseIDs) == 0 {
		return nil
	}

	if err := transaction.WithContext(ctx).
		Where("id IN ?", courseIDs).
		Delete(&types.Course{}).Error; err != nil {
		return err
	}
	return nil
}

func (r *courseRepo) FullDeleteByIDs(ctx context.Context, tx *gorm.DB, courseIDs []uuid.UUID) error {
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}

	if len(courseIDs) == 0 {
		return nil
	}

	if err := transaction.WithContext(ctx).
		Unscoped().
		Where("id IN ?", courseIDs).
		Delete(&types.Course{}).Error; err != nil {
		return err
	}
	return nil
}
