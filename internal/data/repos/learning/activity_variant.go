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

type ActivityVariantRepo interface {
	Create(ctx context.Context, tx *gorm.DB, rows []*types.ActivityVariant) ([]*types.ActivityVariant, error)

	GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.ActivityVariant, error)
	GetByID(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*types.ActivityVariant, error)

	GetByActivityIDs(ctx context.Context, tx *gorm.DB, activityIDs []uuid.UUID) ([]*types.ActivityVariant, error)
	GetByActivityAndVariants(ctx context.Context, tx *gorm.DB, activityID uuid.UUID, variants []string) ([]*types.ActivityVariant, error)
	GetByActivityAndVariant(ctx context.Context, tx *gorm.DB, activityID uuid.UUID, variant string) (*types.ActivityVariant, error)

	Upsert(ctx context.Context, tx *gorm.DB, row *types.ActivityVariant) error
	Update(ctx context.Context, tx *gorm.DB, row *types.ActivityVariant) error
	UpdateFields(ctx context.Context, tx *gorm.DB, id uuid.UUID, updates map[string]interface{}) error

	SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
	SoftDeleteByActivityIDs(ctx context.Context, tx *gorm.DB, activityIDs []uuid.UUID) error
	SoftDeleteByActivityAndVariants(ctx context.Context, tx *gorm.DB, activityID uuid.UUID, variants []string) error
	FullDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
	FullDeleteByActivityIDs(ctx context.Context, tx *gorm.DB, activityIDs []uuid.UUID) error
	FullDeleteByActivityAndVariants(ctx context.Context, tx *gorm.DB, activityID uuid.UUID, variants []string) error
}

type activityVariantRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewActivityVariantRepo(db *gorm.DB, baseLog *logger.Logger) ActivityVariantRepo {
	return &activityVariantRepo{db: db, log: baseLog.With("repo", "ActivityVariantRepo")}
}

func (r *activityVariantRepo) Create(ctx context.Context, tx *gorm.DB, rows []*types.ActivityVariant) ([]*types.ActivityVariant, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return []*types.ActivityVariant{}, nil
	}
	if err := t.WithContext(ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *activityVariantRepo) GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.ActivityVariant, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.ActivityVariant
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

func (r *activityVariantRepo) GetByID(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*types.ActivityVariant, error) {
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

func (r *activityVariantRepo) GetByActivityIDs(ctx context.Context, tx *gorm.DB, activityIDs []uuid.UUID) ([]*types.ActivityVariant, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.ActivityVariant
	if len(activityIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("activity_id IN ?", activityIDs).
		Order("activity_id ASC, variant ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *activityVariantRepo) GetByActivityAndVariants(ctx context.Context, tx *gorm.DB, activityID uuid.UUID, variants []string) ([]*types.ActivityVariant, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.ActivityVariant
	if activityID == uuid.Nil || len(variants) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("activity_id = ? AND variant IN ?", activityID, variants).
		Order("variant ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *activityVariantRepo) GetByActivityAndVariant(ctx context.Context, tx *gorm.DB, activityID uuid.UUID, variant string) (*types.ActivityVariant, error) {
	if activityID == uuid.Nil || variant == "" {
		return nil, nil
	}
	rows, err := r.GetByActivityAndVariants(ctx, tx, activityID, []string{variant})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

func (r *activityVariantRepo) Upsert(ctx context.Context, tx *gorm.DB, row *types.ActivityVariant) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if row == nil || row.ActivityID == uuid.Nil || row.Variant == "" {
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
			Columns: []clause.Column{{Name: "activity_id"}, {Name: "variant"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"content_md",
				"content_json",
				"render_spec",
				"updated_at",
			}),
		}).
		Create(row).Error
}

func (r *activityVariantRepo) Update(ctx context.Context, tx *gorm.DB, row *types.ActivityVariant) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if row == nil {
		return nil
	}
	return t.WithContext(ctx).Save(row).Error
}

func (r *activityVariantRepo) UpdateFields(ctx context.Context, tx *gorm.DB, id uuid.UUID, updates map[string]interface{}) error {
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
		Model(&types.ActivityVariant{}).
		Where("id = ?", id).
		Updates(updates).Error
}

func (r *activityVariantRepo) SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(ctx).Where("id IN ?", ids).Delete(&types.ActivityVariant{}).Error
}

func (r *activityVariantRepo) SoftDeleteByActivityIDs(ctx context.Context, tx *gorm.DB, activityIDs []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(activityIDs) == 0 {
		return nil
	}
	return t.WithContext(ctx).Where("activity_id IN ?", activityIDs).Delete(&types.ActivityVariant{}).Error
}

func (r *activityVariantRepo) SoftDeleteByActivityAndVariants(ctx context.Context, tx *gorm.DB, activityID uuid.UUID, variants []string) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if activityID == uuid.Nil || len(variants) == 0 {
		return nil
	}
	return t.WithContext(ctx).
		Where("activity_id = ? AND variant IN ?", activityID, variants).
		Delete(&types.ActivityVariant{}).Error
}

func (r *activityVariantRepo) FullDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(ctx).Unscoped().Where("id IN ?", ids).Delete(&types.ActivityVariant{}).Error
}

func (r *activityVariantRepo) FullDeleteByActivityIDs(ctx context.Context, tx *gorm.DB, activityIDs []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(activityIDs) == 0 {
		return nil
	}
	return t.WithContext(ctx).Unscoped().Where("activity_id IN ?", activityIDs).Delete(&types.ActivityVariant{}).Error
}

func (r *activityVariantRepo) FullDeleteByActivityAndVariants(ctx context.Context, tx *gorm.DB, activityID uuid.UUID, variants []string) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if activityID == uuid.Nil || len(variants) == 0 {
		return nil
	}
	return t.WithContext(ctx).
		Unscoped().
		Where("activity_id = ? AND variant IN ?", activityID, variants).
		Delete(&types.ActivityVariant{}).Error
}
