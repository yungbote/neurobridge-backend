package learning

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type CourseTagRepo interface {
	Create(ctx context.Context, tx *gorm.DB, rows []*types.CourseTag) ([]*types.CourseTag, error)
	CreateIgnoreDuplicates(ctx context.Context, tx *gorm.DB, rows []*types.CourseTag) (int, error)

	GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.CourseTag, error)
	GetByID(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*types.CourseTag, error)
	GetByCourseIDs(ctx context.Context, tx *gorm.DB, courseIDs []uuid.UUID) ([]*types.CourseTag, error)
	GetByCourseID(ctx context.Context, tx *gorm.DB, courseID uuid.UUID) ([]*types.CourseTag, error)
	GetByTags(ctx context.Context, tx *gorm.DB, tags []string) ([]*types.CourseTag, error)
	GetByCourseIDAndTags(ctx context.Context, tx *gorm.DB, courseID uuid.UUID, tags []string) ([]*types.CourseTag, error)

	UpdateFields(ctx context.Context, tx *gorm.DB, id uuid.UUID, updates map[string]interface{}) error

	SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
	SoftDeleteByCourseIDs(ctx context.Context, tx *gorm.DB, courseIDs []uuid.UUID) error
	SoftDeleteByCourseIDAndTags(ctx context.Context, tx *gorm.DB, courseID uuid.UUID, tags []string) error
	FullDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
	FullDeleteByCourseIDs(ctx context.Context, tx *gorm.DB, courseIDs []uuid.UUID) error
	FullDeleteByCourseIDAndTags(ctx context.Context, tx *gorm.DB, courseID uuid.UUID, tags []string) error
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

func (r *courseTagRepo) CreateIgnoreDuplicates(ctx context.Context, tx *gorm.DB, rows []*types.CourseTag) (int, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return 0, nil
	}
	res := t.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "course_id"}, {Name: "tag"}},
			DoNothing: true,
		}).
		Create(&rows)
	if res.Error != nil {
		return 0, res.Error
	}
	return int(res.RowsAffected), nil
}

func (r *courseTagRepo) GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.CourseTag, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.CourseTag
	if len(ids) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).Where("id IN ?", ids).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *courseTagRepo) GetByID(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*types.CourseTag, error) {
	if id == uuid.Nil {
		return nil, nil
	}
	rows, err := r.GetByIDs(ctx, tx, []uuid.UUID{id})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

func (r *courseTagRepo) GetByCourseIDs(ctx context.Context, tx *gorm.DB, courseIDs []uuid.UUID) ([]*types.CourseTag, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.CourseTag
	if len(courseIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("course_id IN ?", courseIDs).
		Order("course_id ASC, tag ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *courseTagRepo) GetByCourseID(ctx context.Context, tx *gorm.DB, courseID uuid.UUID) ([]*types.CourseTag, error) {
	if courseID == uuid.Nil {
		return []*types.CourseTag{}, nil
	}
	return r.GetByCourseIDs(ctx, tx, []uuid.UUID{courseID})
}

func (r *courseTagRepo) GetByTags(ctx context.Context, tx *gorm.DB, tags []string) ([]*types.CourseTag, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.CourseTag
	if len(tags) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("tag IN ?", tags).
		Order("tag ASC, course_id ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *courseTagRepo) GetByCourseIDAndTags(ctx context.Context, tx *gorm.DB, courseID uuid.UUID, tags []string) ([]*types.CourseTag, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.CourseTag
	if courseID == uuid.Nil || len(tags) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("course_id = ? AND tag IN ?", courseID, tags).
		Order("tag ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *courseTagRepo) UpdateFields(ctx context.Context, tx *gorm.DB, id uuid.UUID, updates map[string]interface{}) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if id == uuid.Nil {
		return nil
	}
	if updates == nil {
		updates = map[string]interface{}{}
	}
	if _, ok := updates["updated_at"]; !ok {
		updates["updated_at"] = time.Now().UTC()
	}
	return t.WithContext(ctx).
		Model(&types.CourseTag{}).
		Where("id = ?", id).
		Updates(updates).Error
}

func (r *courseTagRepo) SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(ctx).Where("id IN ?", ids).Delete(&types.CourseTag{}).Error
}

func (r *courseTagRepo) SoftDeleteByCourseIDs(ctx context.Context, tx *gorm.DB, courseIDs []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(courseIDs) == 0 {
		return nil
	}
	return t.WithContext(ctx).Where("course_id IN ?", courseIDs).Delete(&types.CourseTag{}).Error
}

func (r *courseTagRepo) SoftDeleteByCourseIDAndTags(ctx context.Context, tx *gorm.DB, courseID uuid.UUID, tags []string) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if courseID == uuid.Nil || len(tags) == 0 {
		return nil
	}
	return t.WithContext(ctx).
		Where("course_id = ? AND tag IN ?", courseID, tags).
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
	return t.WithContext(ctx).Unscoped().Where("id IN ?", ids).Delete(&types.CourseTag{}).Error
}

func (r *courseTagRepo) FullDeleteByCourseIDs(ctx context.Context, tx *gorm.DB, courseIDs []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(courseIDs) == 0 {
		return nil
	}
	return t.WithContext(ctx).Unscoped().Where("course_id IN ?", courseIDs).Delete(&types.CourseTag{}).Error
}

func (r *courseTagRepo) FullDeleteByCourseIDAndTags(ctx context.Context, tx *gorm.DB, courseID uuid.UUID, tags []string) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if courseID == uuid.Nil || len(tags) == 0 {
		return nil
	}
	return t.WithContext(ctx).
		Unscoped().
		Where("course_id = ? AND tag IN ?", courseID, tags).
		Delete(&types.CourseTag{}).Error
}
