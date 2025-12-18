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

type LessonConceptRepo interface {
	Create(ctx context.Context, tx *gorm.DB, rows []*types.LessonConcept) ([]*types.LessonConcept, error)
	CreateIgnoreDuplicates(ctx context.Context, tx *gorm.DB, rows []*types.LessonConcept) (int, error)

	GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.LessonConcept, error)
	GetByID(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*types.LessonConcept, error)
	GetByLessonIDs(ctx context.Context, tx *gorm.DB, lessonIDs []uuid.UUID) ([]*types.LessonConcept, error)
	GetByCourseConceptIDs(ctx context.Context, tx *gorm.DB, courseConceptIDs []uuid.UUID) ([]*types.LessonConcept, error)

	UpdateFields(ctx context.Context, tx *gorm.DB, id uuid.UUID, updates map[string]interface{}) error

	SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
	SoftDeleteByLessonIDs(ctx context.Context, tx *gorm.DB, lessonIDs []uuid.UUID) error
	SoftDeleteByCourseConceptIDs(ctx context.Context, tx *gorm.DB, courseConceptIDs []uuid.UUID) error
	FullDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
	FullDeleteByLessonIDs(ctx context.Context, tx *gorm.DB, lessonIDs []uuid.UUID) error
	FullDeleteByCourseConceptIDs(ctx context.Context, tx *gorm.DB, courseConceptIDs []uuid.UUID) error
}

type lessonConceptRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewLessonConceptRepo(db *gorm.DB, baseLog *logger.Logger) LessonConceptRepo {
	return &lessonConceptRepo{db: db, log: baseLog.With("repo", "LessonConceptRepo")}
}

func (r *lessonConceptRepo) Create(ctx context.Context, tx *gorm.DB, rows []*types.LessonConcept) ([]*types.LessonConcept, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return []*types.LessonConcept{}, nil
	}
	if err := t.WithContext(ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *lessonConceptRepo) CreateIgnoreDuplicates(ctx context.Context, tx *gorm.DB, rows []*types.LessonConcept) (int, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return 0, nil
	}
	res := t.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "lesson_id"}, {Name: "course_concept_id"}},
			DoNothing: true,
		}).
		Create(&rows)
	if res.Error != nil {
		return 0, res.Error
	}
	return int(res.RowsAffected), nil
}

func (r *lessonConceptRepo) GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.LessonConcept, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.LessonConcept
	if len(ids) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).Where("id IN ?", ids).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *lessonConceptRepo) GetByID(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*types.LessonConcept, error) {
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

func (r *lessonConceptRepo) GetByLessonIDs(ctx context.Context, tx *gorm.DB, lessonIDs []uuid.UUID) ([]*types.LessonConcept, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.LessonConcept
	if len(lessonIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("lesson_id IN ?", lessonIDs).
		Order("lesson_id ASC, created_at ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *lessonConceptRepo) GetByCourseConceptIDs(ctx context.Context, tx *gorm.DB, courseConceptIDs []uuid.UUID) ([]*types.LessonConcept, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.LessonConcept
	if len(courseConceptIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("course_concept_id IN ?", courseConceptIDs).
		Order("course_concept_id ASC, created_at ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *lessonConceptRepo) UpdateFields(ctx context.Context, tx *gorm.DB, id uuid.UUID, updates map[string]interface{}) error {
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
		Model(&types.LessonConcept{}).
		Where("id = ?", id).
		Updates(updates).Error
}

func (r *lessonConceptRepo) SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(ctx).Where("id IN ?", ids).Delete(&types.LessonConcept{}).Error
}

func (r *lessonConceptRepo) SoftDeleteByLessonIDs(ctx context.Context, tx *gorm.DB, lessonIDs []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(lessonIDs) == 0 {
		return nil
	}
	return t.WithContext(ctx).Where("lesson_id IN ?", lessonIDs).Delete(&types.LessonConcept{}).Error
}

func (r *lessonConceptRepo) SoftDeleteByCourseConceptIDs(ctx context.Context, tx *gorm.DB, courseConceptIDs []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(courseConceptIDs) == 0 {
		return nil
	}
	return t.WithContext(ctx).Where("course_concept_id IN ?", courseConceptIDs).Delete(&types.LessonConcept{}).Error
}

func (r *lessonConceptRepo) FullDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(ctx).Unscoped().Where("id IN ?", ids).Delete(&types.LessonConcept{}).Error
}

func (r *lessonConceptRepo) FullDeleteByLessonIDs(ctx context.Context, tx *gorm.DB, lessonIDs []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(lessonIDs) == 0 {
		return nil
	}
	return t.WithContext(ctx).Unscoped().Where("lesson_id IN ?", lessonIDs).Delete(&types.LessonConcept{}).Error
}

func (r *lessonConceptRepo) FullDeleteByCourseConceptIDs(ctx context.Context, tx *gorm.DB, courseConceptIDs []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(courseConceptIDs) == 0 {
		return nil
	}
	return t.WithContext(ctx).Unscoped().Where("course_concept_id IN ?", courseConceptIDs).Delete(&types.LessonConcept{}).Error
}
