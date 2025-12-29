package materials

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type MaterialSetSummaryRepo interface {
	Create(dbc dbctx.Context, rows []*types.MaterialSetSummary) ([]*types.MaterialSetSummary, error)

	GetByIDs(dbc dbctx.Context, ids []uuid.UUID) ([]*types.MaterialSetSummary, error)
	GetByID(dbc dbctx.Context, id uuid.UUID) (*types.MaterialSetSummary, error)

	GetByMaterialSetIDs(dbc dbctx.Context, setIDs []uuid.UUID) ([]*types.MaterialSetSummary, error)
	GetByUserIDs(dbc dbctx.Context, userIDs []uuid.UUID) ([]*types.MaterialSetSummary, error)

	UpsertByMaterialSetID(dbc dbctx.Context, row *types.MaterialSetSummary) error
	UpdateFields(dbc dbctx.Context, id uuid.UUID, updates map[string]interface{}) error

	SoftDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error
	SoftDeleteByMaterialSetIDs(dbc dbctx.Context, setIDs []uuid.UUID) error
	FullDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error
	FullDeleteByMaterialSetIDs(dbc dbctx.Context, setIDs []uuid.UUID) error
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

func (r *materialSetSummaryRepo) Create(dbc dbctx.Context, rows []*types.MaterialSetSummary) ([]*types.MaterialSetSummary, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return []*types.MaterialSetSummary{}, nil
	}
	if err := t.WithContext(dbc.Ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *materialSetSummaryRepo) GetByIDs(dbc dbctx.Context, ids []uuid.UUID) ([]*types.MaterialSetSummary, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.MaterialSetSummary
	if len(ids) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).Where("id IN ?", ids).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *materialSetSummaryRepo) GetByID(dbc dbctx.Context, id uuid.UUID) (*types.MaterialSetSummary, error) {
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

func (r *materialSetSummaryRepo) GetByMaterialSetIDs(dbc dbctx.Context, setIDs []uuid.UUID) ([]*types.MaterialSetSummary, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.MaterialSetSummary
	if len(setIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("material_set_id IN ?", setIDs).
		Order("material_set_id ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *materialSetSummaryRepo) GetByUserIDs(dbc dbctx.Context, userIDs []uuid.UUID) ([]*types.MaterialSetSummary, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.MaterialSetSummary
	if len(userIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("user_id IN ?", userIDs).
		Order("user_id ASC, updated_at DESC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *materialSetSummaryRepo) UpsertByMaterialSetID(dbc dbctx.Context, row *types.MaterialSetSummary) error {
	t := dbc.Tx
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

	return t.WithContext(dbc.Ctx).
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

func (r *materialSetSummaryRepo) UpdateFields(dbc dbctx.Context, id uuid.UUID, updates map[string]interface{}) error {
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
		Model(&types.MaterialSetSummary{}).
		Where("id = ?", id).
		Updates(updates).Error
}

func (r *materialSetSummaryRepo) SoftDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Where("id IN ?", ids).Delete(&types.MaterialSetSummary{}).Error
}

func (r *materialSetSummaryRepo) SoftDeleteByMaterialSetIDs(dbc dbctx.Context, setIDs []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(setIDs) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Where("material_set_id IN ?", setIDs).Delete(&types.MaterialSetSummary{}).Error
}

func (r *materialSetSummaryRepo) FullDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Unscoped().Where("id IN ?", ids).Delete(&types.MaterialSetSummary{}).Error
}

func (r *materialSetSummaryRepo) FullDeleteByMaterialSetIDs(dbc dbctx.Context, setIDs []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(setIDs) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Unscoped().Where("material_set_id IN ?", setIDs).Delete(&types.MaterialSetSummary{}).Error
}
