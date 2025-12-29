package learning

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
)

type ActivityCitationRepo interface {
	Create(dbc dbctx.Context, rows []*types.ActivityCitation) ([]*types.ActivityCitation, error)
	CreateIgnoreDuplicates(dbc dbctx.Context, rows []*types.ActivityCitation) (int, error)

	GetByIDs(dbc dbctx.Context, ids []uuid.UUID) ([]*types.ActivityCitation, error)
	GetByID(dbc dbctx.Context, id uuid.UUID) (*types.ActivityCitation, error)

	GetByActivityVariantIDs(dbc dbctx.Context, variantIDs []uuid.UUID) ([]*types.ActivityCitation, error)
	GetByMaterialChunkIDs(dbc dbctx.Context, chunkIDs []uuid.UUID) ([]*types.ActivityCitation, error)

	Upsert(dbc dbctx.Context, row *types.ActivityCitation) error
	Update(dbc dbctx.Context, row *types.ActivityCitation) error
	UpdateFields(dbc dbctx.Context, id uuid.UUID, updates map[string]interface{}) error

	SoftDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error
	SoftDeleteByActivityVariantIDs(dbc dbctx.Context, variantIDs []uuid.UUID) error
	SoftDeleteByMaterialChunkIDs(dbc dbctx.Context, chunkIDs []uuid.UUID) error
	FullDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error
	FullDeleteByActivityVariantIDs(dbc dbctx.Context, variantIDs []uuid.UUID) error
	FullDeleteByMaterialChunkIDs(dbc dbctx.Context, chunkIDs []uuid.UUID) error
}

type activityCitationRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewActivityCitationRepo(db *gorm.DB, baseLog *logger.Logger) ActivityCitationRepo {
	return &activityCitationRepo{db: db, log: baseLog.With("repo", "ActivityCitationRepo")}
}

func (r *activityCitationRepo) Create(dbc dbctx.Context, rows []*types.ActivityCitation) ([]*types.ActivityCitation, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return []*types.ActivityCitation{}, nil
	}
	if err := t.WithContext(dbc.Ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *activityCitationRepo) CreateIgnoreDuplicates(dbc dbctx.Context, rows []*types.ActivityCitation) (int, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return 0, nil
	}
	res := t.WithContext(dbc.Ctx).
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

func (r *activityCitationRepo) GetByIDs(dbc dbctx.Context, ids []uuid.UUID) ([]*types.ActivityCitation, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.ActivityCitation
	if len(ids) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("id IN ?", ids).
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *activityCitationRepo) GetByID(dbc dbctx.Context, id uuid.UUID) (*types.ActivityCitation, error) {
	if id == uuid.Nil {
		return nil, nil
	}
	rows, err := r.GetByIDs(dbc, []uuid.UUID{id})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

func (r *activityCitationRepo) GetByActivityVariantIDs(dbc dbctx.Context, variantIDs []uuid.UUID) ([]*types.ActivityCitation, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.ActivityCitation
	if len(variantIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("activity_variant_id IN ?", variantIDs).
		Order("activity_variant_id ASC, created_at ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *activityCitationRepo) GetByMaterialChunkIDs(dbc dbctx.Context, chunkIDs []uuid.UUID) ([]*types.ActivityCitation, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.ActivityCitation
	if len(chunkIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("material_chunk_id IN ?", chunkIDs).
		Order("material_chunk_id ASC, created_at ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *activityCitationRepo) Upsert(dbc dbctx.Context, row *types.ActivityCitation) error {
	t := dbc.Tx
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

	return t.WithContext(dbc.Ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "activity_variant_id"}, {Name: "material_chunk_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"kind",
				"updated_at",
			}),
		}).
		Create(row).Error
}

func (r *activityCitationRepo) Update(dbc dbctx.Context, row *types.ActivityCitation) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if row == nil {
		return nil
	}
	return t.WithContext(dbc.Ctx).Save(row).Error
}

func (r *activityCitationRepo) UpdateFields(dbc dbctx.Context, id uuid.UUID, updates map[string]interface{}) error {
	t := dbc.Tx
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
	return t.WithContext(dbc.Ctx).
		Model(&types.ActivityCitation{}).
		Where("id = ?", id).
		Updates(updates).Error
}

func (r *activityCitationRepo) SoftDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Where("id IN ?", ids).Delete(&types.ActivityCitation{}).Error
}

func (r *activityCitationRepo) SoftDeleteByActivityVariantIDs(dbc dbctx.Context, variantIDs []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(variantIDs) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Where("activity_variant_id IN ?", variantIDs).Delete(&types.ActivityCitation{}).Error
}

func (r *activityCitationRepo) SoftDeleteByMaterialChunkIDs(dbc dbctx.Context, chunkIDs []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(chunkIDs) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Where("material_chunk_id IN ?", chunkIDs).Delete(&types.ActivityCitation{}).Error
}

func (r *activityCitationRepo) FullDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Unscoped().Where("id IN ?", ids).Delete(&types.ActivityCitation{}).Error
}

func (r *activityCitationRepo) FullDeleteByActivityVariantIDs(dbc dbctx.Context, variantIDs []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(variantIDs) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Unscoped().Where("activity_variant_id IN ?", variantIDs).Delete(&types.ActivityCitation{}).Error
}

func (r *activityCitationRepo) FullDeleteByMaterialChunkIDs(dbc dbctx.Context, chunkIDs []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(chunkIDs) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Unscoped().Where("material_chunk_id IN ?", chunkIDs).Delete(&types.ActivityCitation{}).Error
}
