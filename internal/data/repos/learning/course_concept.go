package learning

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type CourseConceptRepo interface {
	Create(ctx context.Context, tx *gorm.DB, rows []*types.CourseConcept) ([]*types.CourseConcept, error)

	GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.CourseConcept, error)
	GetByID(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*types.CourseConcept, error)

	GetByCourseIDs(ctx context.Context, tx *gorm.DB, courseIDs []uuid.UUID) ([]*types.CourseConcept, error)
	GetByCourseAndKeys(ctx context.Context, tx *gorm.DB, courseID uuid.UUID, keys []string) ([]*types.CourseConcept, error)
	GetByCourseAndKey(ctx context.Context, tx *gorm.DB, courseID uuid.UUID, key string) (*types.CourseConcept, error)
	GetByCourseAndParent(ctx context.Context, tx *gorm.DB, courseID uuid.UUID, parentID *uuid.UUID) ([]*types.CourseConcept, error)

	UpsertByCourseAndKey(ctx context.Context, tx *gorm.DB, row *types.CourseConcept) error
	Update(ctx context.Context, tx *gorm.DB, row *types.CourseConcept) error
	UpdateFields(ctx context.Context, tx *gorm.DB, id uuid.UUID, updates map[string]interface{}) error

	SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
	SoftDeleteByCourseIDs(ctx context.Context, tx *gorm.DB, courseIDs []uuid.UUID) error
	FullDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
	FullDeleteByCourseIDs(ctx context.Context, tx *gorm.DB, courseIDs []uuid.UUID) error
}

type courseConceptRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewCourseConceptRepo(db *gorm.DB, baseLog *logger.Logger) CourseConceptRepo {
	return &courseConceptRepo{db: db, log: baseLog.With("repo", "CourseConceptRepo")}
}

func (r *courseConceptRepo) Create(ctx context.Context, tx *gorm.DB, rows []*types.CourseConcept) ([]*types.CourseConcept, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return []*types.CourseConcept{}, nil
	}
	if err := t.WithContext(ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *courseConceptRepo) GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.CourseConcept, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.CourseConcept
	if len(ids) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).Where("id IN ?", ids).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *courseConceptRepo) GetByID(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*types.CourseConcept, error) {
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

func (r *courseConceptRepo) GetByCourseIDs(ctx context.Context, tx *gorm.DB, courseIDs []uuid.UUID) ([]*types.CourseConcept, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.CourseConcept
	if len(courseIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("course_id IN ?", courseIDs).
		Order("course_id ASC, depth ASC, sort_index ASC, key ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *courseConceptRepo) GetByCourseAndKeys(ctx context.Context, tx *gorm.DB, courseID uuid.UUID, keys []string) ([]*types.CourseConcept, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.CourseConcept
	if courseID == uuid.Nil || len(keys) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("course_id = ? AND key IN ?", courseID, keys).
		Order("depth ASC, sort_index ASC, key ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *courseConceptRepo) GetByCourseAndKey(ctx context.Context, tx *gorm.DB, courseID uuid.UUID, key string) (*types.CourseConcept, error) {
	if courseID == uuid.Nil || key == "" {
		return nil, nil
	}
	rows, err := r.GetByCourseAndKeys(ctx, tx, courseID, []string{key})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

func (r *courseConceptRepo) GetByCourseAndParent(ctx context.Context, tx *gorm.DB, courseID uuid.UUID, parentID *uuid.UUID) ([]*types.CourseConcept, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.CourseConcept
	if courseID == uuid.Nil {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("course_id = ? AND parent_id IS NOT DISTINCT FROM ?", courseID, parentID).
		Order("sort_index ASC, key ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *courseConceptRepo) UpsertByCourseAndKey(ctx context.Context, tx *gorm.DB, row *types.CourseConcept) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if row == nil || row.CourseID == uuid.Nil || row.Key == "" {
		return nil
	}
	row.UpdatedAt = time.Now().UTC()

	// Upsert by (course_id, key). We use a find+assign to avoid relying on a specific unique index.
	return t.WithContext(ctx).
		Where("course_id = ? AND key = ?", row.CourseID, row.Key).
		Assign(row).
		FirstOrCreate(row).Error
}

func (r *courseConceptRepo) Update(ctx context.Context, tx *gorm.DB, row *types.CourseConcept) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if row == nil {
		return nil
	}
	return t.WithContext(ctx).Save(row).Error
}

func (r *courseConceptRepo) UpdateFields(ctx context.Context, tx *gorm.DB, id uuid.UUID, updates map[string]interface{}) error {
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
		Model(&types.CourseConcept{}).
		Where("id = ?", id).
		Updates(updates).Error
}

func (r *courseConceptRepo) SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(ctx).Where("id IN ?", ids).Delete(&types.CourseConcept{}).Error
}

func (r *courseConceptRepo) SoftDeleteByCourseIDs(ctx context.Context, tx *gorm.DB, courseIDs []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(courseIDs) == 0 {
		return nil
	}
	return t.WithContext(ctx).Where("course_id IN ?", courseIDs).Delete(&types.CourseConcept{}).Error
}

func (r *courseConceptRepo) FullDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(ctx).Unscoped().Where("id IN ?", ids).Delete(&types.CourseConcept{}).Error
}

func (r *courseConceptRepo) FullDeleteByCourseIDs(ctx context.Context, tx *gorm.DB, courseIDs []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(courseIDs) == 0 {
		return nil
	}
	return t.WithContext(ctx).Unscoped().Where("course_id IN ?", courseIDs).Delete(&types.CourseConcept{}).Error
}
