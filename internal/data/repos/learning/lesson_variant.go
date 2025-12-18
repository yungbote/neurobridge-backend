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

type LessonVariantRepo interface {
	Create(ctx context.Context, tx *gorm.DB, rows []*types.LessonVariant) ([]*types.LessonVariant, error)

	GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.LessonVariant, error)
	GetByID(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*types.LessonVariant, error)
	GetByLessonIDs(ctx context.Context, tx *gorm.DB, lessonIDs []uuid.UUID) ([]*types.LessonVariant, error)
	GetByLessonAndVariants(ctx context.Context, tx *gorm.DB, lessonID uuid.UUID, variants []string) ([]*types.LessonVariant, error)
	GetByLessonAndVariant(ctx context.Context, tx *gorm.DB, lessonID uuid.UUID, variant string) (*types.LessonVariant, error)

	Upsert(ctx context.Context, tx *gorm.DB, row *types.LessonVariant) error
	Update(ctx context.Context, tx *gorm.DB, row *types.LessonVariant) error
	UpdateFields(ctx context.Context, tx *gorm.DB, id uuid.UUID, updates map[string]interface{}) error

	SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
	SoftDeleteByLessonIDs(ctx context.Context, tx *gorm.DB, lessonIDs []uuid.UUID) error
	SoftDeleteByLessonAndVariants(ctx context.Context, tx *gorm.DB, lessonID uuid.UUID, variants []string) error
	FullDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
	FullDeleteByLessonIDs(ctx context.Context, tx *gorm.DB, lessonIDs []uuid.UUID) error
	FullDeleteByLessonAndVariants(ctx context.Context, tx *gorm.DB, lessonID uuid.UUID, variants []string) error
}

type lessonVariantRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewLessonVariantRepo(db *gorm.DB, baseLog *logger.Logger) LessonVariantRepo {
	return &lessonVariantRepo{db: db, log: baseLog.With("repo", "LessonVariantRepo")}
}

func (r *lessonVariantRepo) Create(ctx context.Context, tx *gorm.DB, rows []*types.LessonVariant) ([]*types.LessonVariant, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return []*types.LessonVariant{}, nil
	}
	if err := t.WithContext(ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *lessonVariantRepo) GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.LessonVariant, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.LessonVariant
	if len(ids) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).Where("id IN ?", ids).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *lessonVariantRepo) GetByID(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*types.LessonVariant, error) {
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

func (r *lessonVariantRepo) GetByLessonIDs(ctx context.Context, tx *gorm.DB, lessonIDs []uuid.UUID) ([]*types.LessonVariant, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.LessonVariant
	if len(lessonIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("lesson_id IN ?", lessonIDs).
		Order("lesson_id ASC, variant ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *lessonVariantRepo) GetByLessonAndVariants(ctx context.Context, tx *gorm.DB, lessonID uuid.UUID, variants []string) ([]*types.LessonVariant, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.LessonVariant
	if lessonID == uuid.Nil || len(variants) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("lesson_id = ? AND variant IN ?", lessonID, variants).
		Order("variant ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *lessonVariantRepo) GetByLessonAndVariant(ctx context.Context, tx *gorm.DB, lessonID uuid.UUID, variant string) (*types.LessonVariant, error) {
	if lessonID == uuid.Nil || variant == "" {
		return nil, nil
	}
	rows, err := r.GetByLessonAndVariants(ctx, tx, lessonID, []string{variant})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

func (r *lessonVariantRepo) Upsert(ctx context.Context, tx *gorm.DB, row *types.LessonVariant) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if row == nil || row.LessonID == uuid.Nil || row.Variant == "" {
		return nil
	}
	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	if row.UpdatedAt.IsZero() {
		row.UpdatedAt = time.Now().UTC()
	}

	return t.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "lesson_id"}, {Name: "variant"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"content_md",
				"updated_at",
			}),
		}).
		Create(row).Error
}

func (r *lessonVariantRepo) Update(ctx context.Context, tx *gorm.DB, row *types.LessonVariant) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if row == nil {
		return nil
	}
	return t.WithContext(ctx).Save(row).Error
}

func (r *lessonVariantRepo) UpdateFields(ctx context.Context, tx *gorm.DB, id uuid.UUID, updates map[string]interface{}) error {
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
		Model(&types.LessonVariant{}).
		Where("id = ?", id).
		Updates(updates).Error
}

func (r *lessonVariantRepo) SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(ctx).Where("id IN ?", ids).Delete(&types.LessonVariant{}).Error
}

func (r *lessonVariantRepo) SoftDeleteByLessonIDs(ctx context.Context, tx *gorm.DB, lessonIDs []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(lessonIDs) == 0 {
		return nil
	}
	return t.WithContext(ctx).Where("lesson_id IN ?", lessonIDs).Delete(&types.LessonVariant{}).Error
}

func (r *lessonVariantRepo) SoftDeleteByLessonAndVariants(ctx context.Context, tx *gorm.DB, lessonID uuid.UUID, variants []string) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if lessonID == uuid.Nil || len(variants) == 0 {
		return nil
	}
	return t.WithContext(ctx).
		Where("lesson_id = ? AND variant IN ?", lessonID, variants).
		Delete(&types.LessonVariant{}).Error
}

func (r *lessonVariantRepo) FullDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(ctx).Unscoped().Where("id IN ?", ids).Delete(&types.LessonVariant{}).Error
}

func (r *lessonVariantRepo) FullDeleteByLessonIDs(ctx context.Context, tx *gorm.DB, lessonIDs []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(lessonIDs) == 0 {
		return nil
	}
	return t.WithContext(ctx).Unscoped().Where("lesson_id IN ?", lessonIDs).Delete(&types.LessonVariant{}).Error
}

func (r *lessonVariantRepo) FullDeleteByLessonAndVariants(ctx context.Context, tx *gorm.DB, lessonID uuid.UUID, variants []string) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if lessonID == uuid.Nil || len(variants) == 0 {
		return nil
	}
	return t.WithContext(ctx).
		Unscoped().
		Where("lesson_id = ? AND variant IN ?", lessonID, variants).
		Delete(&types.LessonVariant{}).Error
}
