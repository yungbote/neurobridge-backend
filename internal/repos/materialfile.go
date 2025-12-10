package repos

import (
  "context"
  "github.com/google/uuid"
  "gorm.io/gorm"
  "github.com/yungbote/neurobridge-backend/internal/logger"
  "github.com/yungbote/neurobridge-backend/internal/types"
)

type MaterialFileRepo interface {
  Create(ctx context.Context, tx *gorm.DB, files []*types.MaterialFile) ([]*types.MaterialFile, error)
  GetByIDs(ctx context.Context, tx *gorm.DB, fileIDs []uuid.UUID) ([]*types.MaterialFile, error)
  GetByMaterialSetIDs(ctx context.Context, tx *gorm.DB, setIDs []uuid.UUID) ([]*types.MaterialFile, error)
  GetByMaterialSetID(ctx context.Context, tx *gorm.DB, setID uuid.UUID) ([]*types.MaterialFile, error)
  SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, fileIDs []uuid.UUID) error
  SoftDeleteByMaterialSetIDs(ctx context.Context, tx *gorm.DB, setIDs []uuid.UUID) error
  FullDeleteByIDs(ctx context.Context, tx *gorm.DB, fileIDs []uuid.UUID) error
  FullDeleteByMaterialSetIDs(ctx context.Context, tx *gorm.DB, setIDs []uuid.UUID) error
}

type materialFileRepo struct {
  db  *gorm.DB
  log *logger.Logger
}

func NewMaterialFileRepo(db *gorm.DB, baseLog *logger.Logger) MaterialFileRepo {
  repoLog := baseLog.With("repo", "MaterialFileRepo")
  return &materialFileRepo{db: db, log: repoLog}
}

func (r *materialFileRepo) Create(ctx context.Context, tx *gorm.DB, files []*types.MaterialFile) ([]*types.MaterialFile, error) {
  transaction := tx
  if transaction == nil {
    transaction = r.db
  }

  if len(files) == 0 {
    return []*types.MaterialFile{}, nil
  }

  if err := transaction.WithContext(ctx).Create(&files).Error; err != nil {
    return nil, err
  }
  return files, nil
}

func (r *materialFileRepo) GetByIDs(ctx context.Context, tx *gorm.DB, fileIDs []uuid.UUID) ([]*types.MaterialFile, error) {
  transaction := tx
  if transaction == nil {
    transaction = r.db
  }

  var results []*types.MaterialFile
  if len(fileIDs) == 0 {
    return results, nil
  }

  if err := transaction.WithContext(ctx).
    Where("id IN ?", fileIDs).
    Find(&results).Error; err != nil {
    return nil, err
  }
  return results, nil
}

func (r *materialFileRepo) GetByMaterialSetIDs(ctx context.Context, tx *gorm.DB, setIDs []uuid.UUID) ([]*types.MaterialFile, error) {
  transaction := tx
  if transaction == nil {
    transaction = r.db
  }

  var results []*types.MaterialFile
  if len(setIDs) == 0 {
    return results, nil
  }

  if err := transaction.WithContext(ctx).
    Where("material_set_id IN ?", setIDs).
    Find(&results).Error; err != nil {
    return nil, err
  }
  return results, nil
}

func (r *materialFileRepo) GetByMaterialSetID(ctx context.Context, tx *gorm.DB, setID uuid.UUID) ([]*types.MaterialFile, error) {
  return r.GetByMaterialSetIDs(ctx, tx, []uuid.UUID{setID})
}

func (r *materialFileRepo) SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, fileIDs []uuid.UUID) error {
  transaction := tx
  if transaction == nil {
    transaction = r.db
  }

  if len(fileIDs) == 0 {
    return nil
  }

  if err := transaction.WithContext(ctx).
    Where("id IN ?", fileIDs).
    Delete(&types.MaterialFile{}).Error; err != nil {
    return err
  }
  return nil
}

func (r *materialFileRepo) SoftDeleteByMaterialSetIDs(ctx context.Context, tx *gorm.DB, setIDs []uuid.UUID) error {
  transaction := tx
  if transaction == nil {
    transaction = r.db
  }

  if len(setIDs) == 0 {
    return nil
  }

  if err := transaction.WithContext(ctx).
    Where("material_set_id IN ?", setIDs).
    Delete(&types.MaterialFile{}).Error; err != nil {
    return err
  }
  return nil
}

func (r *materialFileRepo) FullDeleteByIDs(ctx context.Context, tx *gorm.DB, fileIDs []uuid.UUID) error {
  transaction := tx
  if transaction == nil {
    transaction = r.db
  }

  if len(fileIDs) == 0 {
    return nil
  }

  if err := transaction.WithContext(ctx).
    Unscoped().
    Where("id IN ?", fileIDs).
    Delete(&types.MaterialFile{}).Error; err != nil {
    return err
  }
  return nil
}

func (r *materialFileRepo) FullDeleteByMaterialSetIDs(ctx context.Context, tx *gorm.DB, setIDs []uuid.UUID) error {
  transaction := tx
  if transaction == nil {
    transaction = r.db
  }

  if len(setIDs) == 0 {
    return nil
  }

  if err := transaction.WithContext(ctx).
    Unscoped().
    Where("material_set_id IN ?", setIDs).
    Delete(&types.MaterialFile{}).Error; err != nil {
    return err
  }
  return nil
}










