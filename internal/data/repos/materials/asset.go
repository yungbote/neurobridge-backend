package materials

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type AssetRepo interface {
	Create(dbc dbctx.Context, rows []*types.Asset) ([]*types.Asset, error)

	GetByIDs(dbc dbctx.Context, ids []uuid.UUID) ([]*types.Asset, error)
	GetByID(dbc dbctx.Context, id uuid.UUID) (*types.Asset, error)

	GetByOwner(dbc dbctx.Context, ownerType string, ownerID uuid.UUID) ([]*types.Asset, error)
	GetByOwnerIDs(dbc dbctx.Context, ownerType string, ownerIDs []uuid.UUID) ([]*types.Asset, error)
	GetByStorageKeys(dbc dbctx.Context, storageKeys []string) ([]*types.Asset, error)
	GetByKinds(dbc dbctx.Context, kinds []string) ([]*types.Asset, error)

	Update(dbc dbctx.Context, row *types.Asset) error
	UpdateFields(dbc dbctx.Context, id uuid.UUID, updates map[string]interface{}) error

	SoftDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error
	SoftDeleteByOwner(dbc dbctx.Context, ownerType string, ownerID uuid.UUID) error
	SoftDeleteByOwnerIDs(dbc dbctx.Context, ownerType string, ownerIDs []uuid.UUID) error
	FullDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error
	FullDeleteByOwner(dbc dbctx.Context, ownerType string, ownerID uuid.UUID) error
	FullDeleteByOwnerIDs(dbc dbctx.Context, ownerType string, ownerIDs []uuid.UUID) error
}

type assetRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewAssetRepo(db *gorm.DB, baseLog *logger.Logger) AssetRepo {
	return &assetRepo{db: db, log: baseLog.With("repo", "AssetRepo")}
}

func (r *assetRepo) Create(dbc dbctx.Context, rows []*types.Asset) ([]*types.Asset, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return []*types.Asset{}, nil
	}
	if err := t.WithContext(dbc.Ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *assetRepo) GetByIDs(dbc dbctx.Context, ids []uuid.UUID) ([]*types.Asset, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.Asset
	if len(ids) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).Where("id IN ?", ids).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *assetRepo) GetByID(dbc dbctx.Context, id uuid.UUID) (*types.Asset, error) {
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

func (r *assetRepo) GetByOwner(dbc dbctx.Context, ownerType string, ownerID uuid.UUID) ([]*types.Asset, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.Asset
	if ownerType == "" || ownerID == uuid.Nil {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("owner_type = ? AND owner_id = ?", ownerType, ownerID).
		Order("created_at ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *assetRepo) GetByOwnerIDs(dbc dbctx.Context, ownerType string, ownerIDs []uuid.UUID) ([]*types.Asset, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.Asset
	if ownerType == "" || len(ownerIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("owner_type = ? AND owner_id IN ?", ownerType, ownerIDs).
		Order("owner_id ASC, created_at ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *assetRepo) GetByStorageKeys(dbc dbctx.Context, storageKeys []string) ([]*types.Asset, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.Asset
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

func (r *assetRepo) GetByKinds(dbc dbctx.Context, kinds []string) ([]*types.Asset, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.Asset
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

func (r *assetRepo) Update(dbc dbctx.Context, row *types.Asset) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if row == nil {
		return nil
	}
	return t.WithContext(dbc.Ctx).Save(row).Error
}

func (r *assetRepo) UpdateFields(dbc dbctx.Context, id uuid.UUID, updates map[string]interface{}) error {
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
		Model(&types.Asset{}).
		Where("id = ?", id).
		Updates(updates).Error
}

func (r *assetRepo) SoftDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Where("id IN ?", ids).Delete(&types.Asset{}).Error
}

func (r *assetRepo) SoftDeleteByOwner(dbc dbctx.Context, ownerType string, ownerID uuid.UUID) error {
	return r.SoftDeleteByOwnerIDs(dbc, ownerType, []uuid.UUID{ownerID})
}

func (r *assetRepo) SoftDeleteByOwnerIDs(dbc dbctx.Context, ownerType string, ownerIDs []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if ownerType == "" || len(ownerIDs) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).
		Where("owner_type = ? AND owner_id IN ?", ownerType, ownerIDs).
		Delete(&types.Asset{}).Error
}

func (r *assetRepo) FullDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Unscoped().Where("id IN ?", ids).Delete(&types.Asset{}).Error
}

func (r *assetRepo) FullDeleteByOwner(dbc dbctx.Context, ownerType string, ownerID uuid.UUID) error {
	return r.FullDeleteByOwnerIDs(dbc, ownerType, []uuid.UUID{ownerID})
}

func (r *assetRepo) FullDeleteByOwnerIDs(dbc dbctx.Context, ownerType string, ownerIDs []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if ownerType == "" || len(ownerIDs) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).
		Unscoped().
		Where("owner_type = ? AND owner_id IN ?", ownerType, ownerIDs).
		Delete(&types.Asset{}).Error
}
