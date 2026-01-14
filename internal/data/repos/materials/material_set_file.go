package materials

import (
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type MaterialSetFileRepo interface {
	CreateIgnoreDuplicates(dbc dbctx.Context, rows []*types.MaterialSetFile) error
	GetByMaterialSetIDs(dbc dbctx.Context, setIDs []uuid.UUID) ([]*types.MaterialSetFile, error)
}

type materialSetFileRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewMaterialSetFileRepo(db *gorm.DB, baseLog *logger.Logger) MaterialSetFileRepo {
	return &materialSetFileRepo{
		db:  db,
		log: baseLog.With("repo", "MaterialSetFileRepo"),
	}
}

func (r *materialSetFileRepo) CreateIgnoreDuplicates(dbc dbctx.Context, rows []*types.MaterialSetFile) error {
	tx := dbc.Tx
	if tx == nil {
		tx = r.db
	}
	if len(rows) == 0 {
		return nil
	}
	return tx.WithContext(dbc.Ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&rows).Error
}

func (r *materialSetFileRepo) GetByMaterialSetIDs(dbc dbctx.Context, setIDs []uuid.UUID) ([]*types.MaterialSetFile, error) {
	tx := dbc.Tx
	if tx == nil {
		tx = r.db
	}
	out := []*types.MaterialSetFile{}
	if len(setIDs) == 0 {
		return out, nil
	}
	if err := tx.WithContext(dbc.Ctx).
		Where("material_set_id IN ?", setIDs).
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}
