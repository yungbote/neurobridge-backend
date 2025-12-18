package materials

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type MaterialAssetRepo interface {
	Create(ctx context.Context, tx *gorm.DB, rows []*types.MaterialAsset) ([]*types.MaterialAsset, error)

	GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.MaterialAsset, error)
	GetByID(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*types.MaterialAsset, error)
	GetByMaterialFileIDs(ctx context.Context, tx *gorm.DB, fileIDs []uuid.UUID) ([]*types.MaterialAsset, error)
	GetByStorageKeys(ctx context.Context, tx *gorm.DB, storageKeys []string) ([]*types.MaterialAsset, error)
	GetByKinds(ctx context.Context, tx *gorm.DB, kinds []string) ([]*types.MaterialAsset, error)

	Update(ctx context.Context, tx *gorm.DB, row *types.MaterialAsset) error
	UpdateFields(ctx context.Context, tx *gorm.DB, id uuid.UUID, updates map[string]interface{}) error

	SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
	SoftDeleteByMaterialFileIDs(ctx context.Context, tx *gorm.DB, fileIDs []uuid.UUID) error
	FullDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
	FullDeleteByMaterialFileIDs(ctx context.Context, tx *gorm.DB, fileIDs []uuid.UUID) error
}

type materialAssetRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewMaterialAssetRepo(db *gorm.DB, baseLog *logger.Logger) MaterialAssetRepo {
	return &materialAssetRepo{db: db, log: baseLog.With("repo", "MaterialAssetRepo")}
}

func (r *materialAssetRepo) Create(ctx context.Context, tx *gorm.DB, rows []*types.MaterialAsset) ([]*types.MaterialAsset, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return []*types.MaterialAsset{}, nil
	}
	if err := t.WithContext(ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *materialAssetRepo) GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.MaterialAsset, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.MaterialAsset
	if len(ids) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).Where("id IN ?", ids).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *materialAssetRepo) GetByID(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*types.MaterialAsset, error) {
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

func (r *materialAssetRepo) GetByMaterialFileIDs(ctx context.Context, tx *gorm.DB, fileIDs []uuid.UUID) ([]*types.MaterialAsset, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.MaterialAsset
	if len(fileIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("material_file_id IN ?", fileIDs).
		Order("material_file_id ASC, created_at ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *materialAssetRepo) GetByStorageKeys(ctx context.Context, tx *gorm.DB, storageKeys []string) ([]*types.MaterialAsset, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.MaterialAsset
	if len(storageKeys) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("storage_key IN ?", storageKeys).
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *materialAssetRepo) GetByKinds(ctx context.Context, tx *gorm.DB, kinds []string) ([]*types.MaterialAsset, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.MaterialAsset
	if len(kinds) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("kind IN ?", kinds).
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *materialAssetRepo) Update(ctx context.Context, tx *gorm.DB, row *types.MaterialAsset) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if row == nil {
		return nil
	}
	return t.WithContext(ctx).Save(row).Error
}

func (r *materialAssetRepo) UpdateFields(ctx context.Context, tx *gorm.DB, id uuid.UUID, updates map[string]interface{}) error {
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
		Model(&types.MaterialAsset{}).
		Where("id = ?", id).
		Updates(updates).Error
}

func (r *materialAssetRepo) SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(ctx).Where("id IN ?", ids).Delete(&types.MaterialAsset{}).Error
}

func (r *materialAssetRepo) SoftDeleteByMaterialFileIDs(ctx context.Context, tx *gorm.DB, fileIDs []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(fileIDs) == 0 {
		return nil
	}
	return t.WithContext(ctx).Where("material_file_id IN ?", fileIDs).Delete(&types.MaterialAsset{}).Error
}

func (r *materialAssetRepo) FullDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(ctx).Unscoped().Where("id IN ?", ids).Delete(&types.MaterialAsset{}).Error
}

func (r *materialAssetRepo) FullDeleteByMaterialFileIDs(ctx context.Context, tx *gorm.DB, fileIDs []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(fileIDs) == 0 {
		return nil
	}
	return t.WithContext(ctx).Unscoped().Where("material_file_id IN ?", fileIDs).Delete(&types.MaterialAsset{}).Error
}
