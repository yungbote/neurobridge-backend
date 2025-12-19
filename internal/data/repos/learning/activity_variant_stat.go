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

type ActivityVariantStatRepo interface {
	Create(ctx context.Context, tx *gorm.DB, rows []*types.ActivityVariantStat) ([]*types.ActivityVariantStat, error)

	GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.ActivityVariantStat, error)
	GetByVariantIDs(ctx context.Context, tx *gorm.DB, variantIDs []uuid.UUID) ([]*types.ActivityVariantStat, error)

	Upsert(ctx context.Context, tx *gorm.DB, row *types.ActivityVariantStat) error
	UpdateFields(ctx context.Context, tx *gorm.DB, id uuid.UUID, updates map[string]interface{}) error

	SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
	FullDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
}

type activityVariantStatRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewActivityVariantStatRepo(db *gorm.DB, baseLog *logger.Logger) ActivityVariantStatRepo {
	return &activityVariantStatRepo{db: db, log: baseLog.With("repo", "ActivityVariantStatRepo")}
}

func (r *activityVariantStatRepo) Create(ctx context.Context, tx *gorm.DB, rows []*types.ActivityVariantStat) ([]*types.ActivityVariantStat, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return []*types.ActivityVariantStat{}, nil
	}
	if err := t.WithContext(ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *activityVariantStatRepo) GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.ActivityVariantStat, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.ActivityVariantStat
	if len(ids) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).Where("id IN ?", ids).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *activityVariantStatRepo) GetByVariantIDs(ctx context.Context, tx *gorm.DB, variantIDs []uuid.UUID) ([]*types.ActivityVariantStat, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.ActivityVariantStat
	if len(variantIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("activity_variant_id IN ?", variantIDs).
		Order("activity_variant_id ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *activityVariantStatRepo) Upsert(ctx context.Context, tx *gorm.DB, row *types.ActivityVariantStat) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if row == nil || row.ActivityVariantID == uuid.Nil {
		return nil
	}
	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	row.UpdatedAt = time.Now().UTC()

	return t.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "activity_variant_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"completions",
				"starts",
				"thumbs_up",
				"thumbs_down",
				"avg_score",
				"avg_dwell_ms",
				"last_observed_at",
				"updated_at",
			}),
		}).
		Create(row).Error
}

func (r *activityVariantStatRepo) UpdateFields(ctx context.Context, tx *gorm.DB, id uuid.UUID, updates map[string]interface{}) error {
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
		Model(&types.ActivityVariantStat{}).
		Where("id = ?", id).
		Updates(updates).Error
}

func (r *activityVariantStatRepo) SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(ctx).Where("id IN ?", ids).Delete(&types.ActivityVariantStat{}).Error
}

func (r *activityVariantStatRepo) FullDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(ctx).Unscoped().Where("id IN ?", ids).Delete(&types.ActivityVariantStat{}).Error
}










