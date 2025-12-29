package materials

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type MaterialAssetRepo interface {
	Create(dbc dbctx.Context, rows []*types.MaterialAsset) ([]*types.MaterialAsset, error)

	GetByIDs(dbc dbctx.Context, ids []uuid.UUID) ([]*types.MaterialAsset, error)
	GetByID(dbc dbctx.Context, id uuid.UUID) (*types.MaterialAsset, error)
	GetByMaterialFileIDs(dbc dbctx.Context, fileIDs []uuid.UUID) ([]*types.MaterialAsset, error)
	GetByStorageKeys(dbc dbctx.Context, storageKeys []string) ([]*types.MaterialAsset, error)
	GetByKinds(dbc dbctx.Context, kinds []string) ([]*types.MaterialAsset, error)

	Update(dbc dbctx.Context, row *types.MaterialAsset) error
	UpdateFields(dbc dbctx.Context, id uuid.UUID, updates map[string]interface{}) error

	SoftDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error
	SoftDeleteByMaterialFileIDs(dbc dbctx.Context, fileIDs []uuid.UUID) error
	FullDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error
	FullDeleteByMaterialFileIDs(dbc dbctx.Context, fileIDs []uuid.UUID) error
}

type materialAssetRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewMaterialAssetRepo(db *gorm.DB, baseLog *logger.Logger) MaterialAssetRepo {
	return &materialAssetRepo{db: db, log: baseLog.With("repo", "MaterialAssetRepo")}
}

func (r *materialAssetRepo) Create(dbc dbctx.Context, rows []*types.MaterialAsset) ([]*types.MaterialAsset, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return []*types.MaterialAsset{}, nil
	}
	if err := t.WithContext(dbc.Ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *materialAssetRepo) GetByIDs(dbc dbctx.Context, ids []uuid.UUID) ([]*types.MaterialAsset, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.MaterialAsset
	if len(ids) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).Where("id IN ?", ids).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *materialAssetRepo) GetByID(dbc dbctx.Context, id uuid.UUID) (*types.MaterialAsset, error) {
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

func (r *materialAssetRepo) GetByMaterialFileIDs(dbc dbctx.Context, fileIDs []uuid.UUID) ([]*types.MaterialAsset, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.MaterialAsset
	if len(fileIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("material_file_id IN ?", fileIDs).
		Order("material_file_id ASC, created_at ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *materialAssetRepo) GetByStorageKeys(dbc dbctx.Context, storageKeys []string) ([]*types.MaterialAsset, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.MaterialAsset
	if len(storageKeys) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("storage_key IN ?", storageKeys).
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *materialAssetRepo) GetByKinds(dbc dbctx.Context, kinds []string) ([]*types.MaterialAsset, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.MaterialAsset
	if len(kinds) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("kind IN ?", kinds).
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *materialAssetRepo) Update(dbc dbctx.Context, row *types.MaterialAsset) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if row == nil {
		return nil
	}
	return t.WithContext(dbc.Ctx).Save(row).Error
}

func (r *materialAssetRepo) UpdateFields(dbc dbctx.Context, id uuid.UUID, updates map[string]interface{}) error {
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
		Model(&types.MaterialAsset{}).
		Where("id = ?", id).
		Updates(updates).Error
}

func (r *materialAssetRepo) SoftDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Where("id IN ?", ids).Delete(&types.MaterialAsset{}).Error
}

func (r *materialAssetRepo) SoftDeleteByMaterialFileIDs(dbc dbctx.Context, fileIDs []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(fileIDs) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Where("material_file_id IN ?", fileIDs).Delete(&types.MaterialAsset{}).Error
}

func (r *materialAssetRepo) FullDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Unscoped().Where("id IN ?", ids).Delete(&types.MaterialAsset{}).Error
}

func (r *materialAssetRepo) FullDeleteByMaterialFileIDs(dbc dbctx.Context, fileIDs []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(fileIDs) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Unscoped().Where("material_file_id IN ?", fileIDs).Delete(&types.MaterialAsset{}).Error
}
