package materials

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type MaterialFileRepo interface {
	Create(dbc dbctx.Context, files []*types.MaterialFile) ([]*types.MaterialFile, error)
	GetByIDs(dbc dbctx.Context, fileIDs []uuid.UUID) ([]*types.MaterialFile, error)
	GetByMaterialSetIDs(dbc dbctx.Context, setIDs []uuid.UUID) ([]*types.MaterialFile, error)
	GetByMaterialSetID(dbc dbctx.Context, setID uuid.UUID) ([]*types.MaterialFile, error)
	SoftDeleteByIDs(dbc dbctx.Context, fileIDs []uuid.UUID) error
	SoftDeleteByMaterialSetIDs(dbc dbctx.Context, setIDs []uuid.UUID) error
	FullDeleteByIDs(dbc dbctx.Context, fileIDs []uuid.UUID) error
	FullDeleteByMaterialSetIDs(dbc dbctx.Context, setIDs []uuid.UUID) error
}

type materialFileRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewMaterialFileRepo(db *gorm.DB, baseLog *logger.Logger) MaterialFileRepo {
	repoLog := baseLog.With("repo", "MaterialFileRepo")
	return &materialFileRepo{db: db, log: repoLog}
}

func (r *materialFileRepo) Create(dbc dbctx.Context, files []*types.MaterialFile) ([]*types.MaterialFile, error) {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}

	if len(files) == 0 {
		return []*types.MaterialFile{}, nil
	}

	if err := transaction.WithContext(dbc.Ctx).Create(&files).Error; err != nil {
		return nil, err
	}
	return files, nil
}

func (r *materialFileRepo) GetByIDs(dbc dbctx.Context, fileIDs []uuid.UUID) ([]*types.MaterialFile, error) {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}

	var results []*types.MaterialFile
	if len(fileIDs) == 0 {
		return results, nil
	}

	if err := transaction.WithContext(dbc.Ctx).
		Where("id IN ?", fileIDs).
		Find(&results).Error; err != nil {
		return nil, err
	}
	return results, nil
}

func (r *materialFileRepo) GetByMaterialSetIDs(dbc dbctx.Context, setIDs []uuid.UUID) ([]*types.MaterialFile, error) {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}

	var results []*types.MaterialFile
	if len(setIDs) == 0 {
		return results, nil
	}

	if err := transaction.WithContext(dbc.Ctx).
		Where("material_set_id IN ?", setIDs).
		Find(&results).Error; err != nil {
		return nil, err
	}
	return results, nil
}

func (r *materialFileRepo) GetByMaterialSetID(dbc dbctx.Context, setID uuid.UUID) ([]*types.MaterialFile, error) {
	return r.GetByMaterialSetIDs(dbc, []uuid.UUID{setID})
}

func (r *materialFileRepo) SoftDeleteByIDs(dbc dbctx.Context, fileIDs []uuid.UUID) error {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}

	if len(fileIDs) == 0 {
		return nil
	}

	if err := transaction.WithContext(dbc.Ctx).
		Where("id IN ?", fileIDs).
		Delete(&types.MaterialFile{}).Error; err != nil {
		return err
	}
	return nil
}

func (r *materialFileRepo) SoftDeleteByMaterialSetIDs(dbc dbctx.Context, setIDs []uuid.UUID) error {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}

	if len(setIDs) == 0 {
		return nil
	}

	if err := transaction.WithContext(dbc.Ctx).
		Where("material_set_id IN ?", setIDs).
		Delete(&types.MaterialFile{}).Error; err != nil {
		return err
	}
	return nil
}

func (r *materialFileRepo) FullDeleteByIDs(dbc dbctx.Context, fileIDs []uuid.UUID) error {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}

	if len(fileIDs) == 0 {
		return nil
	}

	if err := transaction.WithContext(dbc.Ctx).
		Unscoped().
		Where("id IN ?", fileIDs).
		Delete(&types.MaterialFile{}).Error; err != nil {
		return err
	}
	return nil
}

func (r *materialFileRepo) FullDeleteByMaterialSetIDs(dbc dbctx.Context, setIDs []uuid.UUID) error {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}

	if len(setIDs) == 0 {
		return nil
	}

	if err := transaction.WithContext(dbc.Ctx).
		Unscoped().
		Where("material_set_id IN ?", setIDs).
		Delete(&types.MaterialFile{}).Error; err != nil {
		return err
	}
	return nil
}
