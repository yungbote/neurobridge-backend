package materials

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type MaterialSetSummaryRepo interface {
	Create(ctx context.Context, tx *gorm.DB, rows []*types.MaterialSetSummary) ([]*types.MaterialSetSummary, error)

	GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.MaterialSetSummary, error)
	GetByID(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*types.MaterialSetSummary, error)

	GetByMaterialSetIDs(ctx context.Context, tx *gorm.DB, setIDs []uuid.UUID) ([]*types.MaterialSetSummary, error)
	GetByUserIDs(ctx context.Context, tx *gorm.DB, userIDs []uuid.UUID) ([]*types.MaterialSetSummary, error)

	UpsertByMaterialSetID(ctx context.Context, tx *gorm.DB, row *types.MaterialSetSummary) error
	UpdateFields(ctx context.Context, tx *gorm.DB, id uuid.UUID, updates map[string]interface{}) error

	SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
	SoftDeleteByMaterialSetIDs(ctx context.Context, tx *gorm.DB, setIDs []uuid.UUID) error
	FullDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
	FullDeleteByMaterialSetIDs(ctx context.Context, tx *gorm.DB, setIDs []uuid.UUID) error
}

type materialSetSummaryRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewMaterialSetSummaryRepo(db *gorm.DB, baseLog *logger.Logger) MaterialSetSummaryRepo {
	return &materialSetSummaryRepo{
		db:  db,
		log: baseLog.With("repo", "MaterialSetSummaryRepo"),
	}
}

func (r *materialSetSummaryRepo) Create(ctx context.Context, tx *gorm.DB, rows []*types.MaterialSetSummary) ([]*types.MaterialSetSummary, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return []*types.MaterialSetSummary{}, nil
	}
	if err := t.WithContext(ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *materialSetSummaryRepo) GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.MaterialSetSummary, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.MaterialSetSummary
	if len(ids) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).Where("id IN ?", ids).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *materialSetSummaryRepo) GetByID(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*types.MaterialSetSummary, error) {
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

func (r *materialSetSummaryRepo) GetByMaterialSetIDs(ctx context.Context, tx *gorm.DB, setIDs []uuid.UUID) ([]*types.MaterialSetSummary, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.MaterialSetSummary
	if len(setIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("material_set_id IN ?", setIDs).
		Order("material_set_id ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *materialSetSummaryRepo) GetByUserIDs(ctx context.Context, tx *gorm.DB, userIDs []uuid.UUID) ([]*types.MaterialSetSummary, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.MaterialSetSummary
	if len(userIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("user_id IN ?", userIDs).
		Order("user_id ASC, updated_at DESC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *materialSetSummaryRepo) UpsertByMaterialSetID(ctx context.Context, tx *gorm.DB, row *types.MaterialSetSummary) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if row == nil || row.MaterialSetID == uuid.Nil {
		return nil
	}
	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	row.UpdatedAt = time.Now().UTC()

	return t.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "material_set_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"user_id",
				"subject",
				"level",
				"summary_md",
				"tags",
				"concept_keys",
				"embedding",
				"vector_id",
				"updated_at",
			}),
		}).
		Create(row).Error
}

func (r *materialSetSummaryRepo) UpdateFields(ctx context.Context, tx *gorm.DB, id uuid.UUID, updates map[string]interface{}) error {
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
		Model(&types.MaterialSetSummary{}).
		Where("id = ?", id).
		Updates(updates).Error
}

func (r *materialSetSummaryRepo) SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(ctx).Where("id IN ?", ids).Delete(&types.MaterialSetSummary{}).Error
}

func (r *materialSetSummaryRepo) SoftDeleteByMaterialSetIDs(ctx context.Context, tx *gorm.DB, setIDs []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(setIDs) == 0 {
		return nil
	}
	return t.WithContext(ctx).Where("material_set_id IN ?", setIDs).Delete(&types.MaterialSetSummary{}).Error
}

func (r *materialSetSummaryRepo) FullDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(ctx).Unscoped().Where("id IN ?", ids).Delete(&types.MaterialSetSummary{}).Error
}

func (r *materialSetSummaryRepo) FullDeleteByMaterialSetIDs(ctx context.Context, tx *gorm.DB, setIDs []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(setIDs) == 0 {
		return nil
	}
	return t.WithContext(ctx).Unscoped().Where("material_set_id IN ?", setIDs).Delete(&types.MaterialSetSummary{}).Error
}










