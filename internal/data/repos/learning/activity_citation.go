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

type ActivityCitationRepo interface {
	Create(ctx context.Context, tx *gorm.DB, rows []*types.ActivityCitation) ([]*types.ActivityCitation, error)
	CreateIgnoreDuplicates(ctx context.Context, tx *gorm.DB, rows []*types.ActivityCitation) (int, error)

	GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.ActivityCitation, error)
	GetByID(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*types.ActivityCitation, error)

	GetByActivityVariantIDs(ctx context.Context, tx *gorm.DB, variantIDs []uuid.UUID) ([]*types.ActivityCitation, error)
	GetByMaterialChunkIDs(ctx context.Context, tx *gorm.DB, chunkIDs []uuid.UUID) ([]*types.ActivityCitation, error)

	Upsert(ctx context.Context, tx *gorm.DB, row *types.ActivityCitation) error
	Update(ctx context.Context, tx *gorm.DB, row *types.ActivityCitation) error
	UpdateFields(ctx context.Context, tx *gorm.DB, id uuid.UUID, updates map[string]interface{}) error

	SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
	SoftDeleteByActivityVariantIDs(ctx context.Context, tx *gorm.DB, variantIDs []uuid.UUID) error
	SoftDeleteByMaterialChunkIDs(ctx context.Context, tx *gorm.DB, chunkIDs []uuid.UUID) error
	FullDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
	FullDeleteByActivityVariantIDs(ctx context.Context, tx *gorm.DB, variantIDs []uuid.UUID) error
	FullDeleteByMaterialChunkIDs(ctx context.Context, tx *gorm.DB, chunkIDs []uuid.UUID) error
}

type activityCitationRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewActivityCitationRepo(db *gorm.DB, baseLog *logger.Logger) ActivityCitationRepo {
	return &activityCitationRepo{db: db, log: baseLog.With("repo", "ActivityCitationRepo")}
}

func (r *activityCitationRepo) Create(ctx context.Context, tx *gorm.DB, rows []*types.ActivityCitation) ([]*types.ActivityCitation, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return []*types.ActivityCitation{}, nil
	}
	if err := t.WithContext(ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *activityCitationRepo) CreateIgnoreDuplicates(ctx context.Context, tx *gorm.DB, rows []*types.ActivityCitation) (int, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return 0, nil
	}
	res := t.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "activity_variant_id"}, {Name: "material_chunk_id"}},
			DoNothing: true,
		}).
		Create(&rows)
	if res.Error != nil {
		return 0, res.Error
	}
	return int(res.RowsAffected), nil
}

func (r *activityCitationRepo) GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.ActivityCitation, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.ActivityCitation
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

func (r *activityCitationRepo) GetByID(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*types.ActivityCitation, error) {
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

func (r *activityCitationRepo) GetByActivityVariantIDs(ctx context.Context, tx *gorm.DB, variantIDs []uuid.UUID) ([]*types.ActivityCitation, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.ActivityCitation
	if len(variantIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("activity_variant_id IN ?", variantIDs).
		Order("activity_variant_id ASC, created_at ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *activityCitationRepo) GetByMaterialChunkIDs(ctx context.Context, tx *gorm.DB, chunkIDs []uuid.UUID) ([]*types.ActivityCitation, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.ActivityCitation
	if len(chunkIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("material_chunk_id IN ?", chunkIDs).
		Order("material_chunk_id ASC, created_at ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *activityCitationRepo) Upsert(ctx context.Context, tx *gorm.DB, row *types.ActivityCitation) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if row == nil || row.ActivityVariantID == uuid.Nil || row.MaterialChunkID == uuid.Nil {
		return nil
	}
	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	row.UpdatedAt = time.Now().UTC()

	return t.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "activity_variant_id"}, {Name: "material_chunk_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"kind",
				"updated_at",
			}),
		}).
		Create(row).Error
}

func (r *activityCitationRepo) Update(ctx context.Context, tx *gorm.DB, row *types.ActivityCitation) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if row == nil {
		return nil
	}
	return t.WithContext(ctx).Save(row).Error
}

func (r *activityCitationRepo) UpdateFields(ctx context.Context, tx *gorm.DB, id uuid.UUID, updates map[string]interface{}) error {
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
		Model(&types.ActivityCitation{}).
		Where("id = ?", id).
		Updates(updates).Error
}

func (r *activityCitationRepo) SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(ctx).Where("id IN ?", ids).Delete(&types.ActivityCitation{}).Error
}

func (r *activityCitationRepo) SoftDeleteByActivityVariantIDs(ctx context.Context, tx *gorm.DB, variantIDs []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(variantIDs) == 0 {
		return nil
	}
	return t.WithContext(ctx).Where("activity_variant_id IN ?", variantIDs).Delete(&types.ActivityCitation{}).Error
}

func (r *activityCitationRepo) SoftDeleteByMaterialChunkIDs(ctx context.Context, tx *gorm.DB, chunkIDs []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(chunkIDs) == 0 {
		return nil
	}
	return t.WithContext(ctx).Where("material_chunk_id IN ?", chunkIDs).Delete(&types.ActivityCitation{}).Error
}

func (r *activityCitationRepo) FullDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(ctx).Unscoped().Where("id IN ?", ids).Delete(&types.ActivityCitation{}).Error
}

func (r *activityCitationRepo) FullDeleteByActivityVariantIDs(ctx context.Context, tx *gorm.DB, variantIDs []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(variantIDs) == 0 {
		return nil
	}
	return t.WithContext(ctx).Unscoped().Where("activity_variant_id IN ?", variantIDs).Delete(&types.ActivityCitation{}).Error
}

func (r *activityCitationRepo) FullDeleteByMaterialChunkIDs(ctx context.Context, tx *gorm.DB, chunkIDs []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(chunkIDs) == 0 {
		return nil
	}
	return t.WithContext(ctx).Unscoped().Where("material_chunk_id IN ?", chunkIDs).Delete(&types.ActivityCitation{}).Error
}
