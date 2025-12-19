package learning

import (
	"context"

	"github.com/google/uuid"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"gorm.io/gorm"
)

type CourseTagRepo interface {
	Create(ctx context.Context, tx *gorm.DB, rows []*types.CourseTag) ([]*types.CourseTag, error)
	GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.CourseTag, error)
	ListByCourseIDs(ctx context.Context, tx *gorm.DB, courseIDs []uuid.UUID) ([]*types.CourseTag, error)
	SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
	SoftDeleteByCourseIDs(ctx context.Context, tx *gorm.DB, courseIDs []uuid.UUID) error
	FullDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
	FullDeleteByCourseIDs(ctx context.Context, tx *gorm.DB, courseIDs []uuid.UUID) error
}

type courseTagRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewCourseTagRepo(db *gorm.DB, baseLog *logger.Logger) CourseTagRepo {
	return &courseTagRepo{db: db, log: baseLog.With("repo", "CourseTagRepo")}
}

func (r *courseTagRepo) Create(ctx context.Context, tx *gorm.DB, rows []*types.CourseTag) ([]*types.CourseTag, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return []*types.CourseTag{}, nil
	}
	if err := t.WithContext(ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *courseTagRepo) GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.CourseTag, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	out := []*types.CourseTag{}
	if len(ids) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("id IN ?", ids).
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *courseTagRepo) ListByCourseIDs(ctx context.Context, tx *gorm.DB, courseIDs []uuid.UUID) ([]*types.CourseTag, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	out := []*types.CourseTag{}
	if len(courseIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("course_id IN ?", courseIDs).
		Order("course_id, tag ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *courseTagRepo) SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(ctx).
		Where("id IN ?", ids).
		Delete(&types.CourseTag{}).Error
}

func (r *courseTagRepo) SoftDeleteByCourseIDs(ctx context.Context, tx *gorm.DB, courseIDs []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(courseIDs) == 0 {
		return nil
	}
	return t.WithContext(ctx).
		Where("course_id IN ?", courseIDs).
		Delete(&types.CourseTag{}).Error
}

func (r *courseTagRepo) FullDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(ctx).
		Unscoped().
		Where("id IN ?", ids).
		Delete(&types.CourseTag{}).Error
}

func (r *courseTagRepo) FullDeleteByCourseIDs(ctx context.Context, tx *gorm.DB, courseIDs []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(courseIDs) == 0 {
		return nil
	}
	return t.WithContext(ctx).
		Unscoped().
		Where("course_id IN ?", courseIDs).
		Delete(&types.CourseTag{}).Error
}
