package repos

import (
  "context"
  "github.com/google/uuid"
  "gorm.io/gorm"
  "github.com/yungbote/neurobridge-backend/internal/logger"
  "github.com/yungbote/neurobridge-backend/internal/types"
)

type LessonAssetRepo interface {
  Create(ctx context.Context, tx *gorm.DB, assets []*types.LessonAsset) ([]*types.LessonAsset, error)
  GetByIDs(ctx context.Context, tx *gorm.DB, assetIDs []uuid.UUID) ([]*types.LessonAsset, error)
  GetByLessonIDs(ctx context.Context, tx *gorm.DB, lessonIDs []uuid.UUID) ([]*types.LessonAsset, error)
  SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, assetIDs []uuid.UUID) error
  SoftDeleteByLessonIDs(ctx context.Context, tx *gorm.DB, lessonIDs []uuid.UUID) error
  FullDeleteByIDs(ctx context.Context, tx *gorm.DB, assetIDs []uuid.UUID) error
  FullDeleteByLessonIDs(ctx context.Context, tx *gorm.DB, lessonIDs []uuid.UUID) error
}

type lessonAssetRepo struct {
  db  *gorm.DB
  log *logger.Logger
}

func NewLessonAssetRepo(db *gorm.DB, baseLog *logger.Logger) LessonAssetRepo {
  repoLog := baseLog.With("repo", "LessonAssetRepo")
  return &lessonAssetRepo{db: db, log: repoLog}
}

func (r *lessonAssetRepo) Create(ctx context.Context, tx *gorm.DB, assets []*types.LessonAsset) ([]*types.LessonAsset, error) {
  transaction := tx
  if transaction == nil {
    transaction = r.db
  }

  if len(assets) == 0 {
    return []*types.LessonAsset{}, nil
  }

  if err := transaction.WithContext(ctx).Create(&assets).Error; err != nil {
    return nil, err
  }
  return assets, nil
}

func (r *lessonAssetRepo) GetByIDs(ctx context.Context, tx *gorm.DB, assetIDs []uuid.UUID) ([]*types.LessonAsset, error) {
  transaction := tx
  if transaction == nil {
    transaction = r.db
  }

  var results []*types.LessonAsset
  if len(assetIDs) == 0 {
    return results, nil
  }

  if err := transaction.WithContext(ctx).
    Where("id IN ?", assetIDs).
    Find(&results).Error; err != nil {
    return nil, err
  }
  return results, nil
}

func (r *lessonAssetRepo) GetByLessonIDs(ctx context.Context, tx *gorm.DB, lessonIDs []uuid.UUID) ([]*types.LessonAsset, error) {
  transaction := tx
  if transaction == nil {
    transaction = r.db
  }

  var results []*types.LessonAsset
  if len(lessonIDs) == 0 {
    return results, nil
  }

  if err := transaction.WithContext(ctx).
    Where("lesson_id IN ?", lessonIDs).
    Find(&results).Error; err != nil {
    return nil, err
  }
  return results, nil
}

func (r *lessonAssetRepo) SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, assetIDs []uuid.UUID) error {
  transaction := tx
  if transaction == nil {
    transaction = r.db
  }

  if len(assetIDs) == 0 {
    return nil
  }

  if err := transaction.WithContext(ctx).
    Where("id IN ?", assetIDs).
    Delete(&types.LessonAsset{}).Error; err != nil {
    return err
  }
  return nil
}

func (r *lessonAssetRepo) SoftDeleteByLessonIDs(ctx context.Context, tx *gorm.DB, lessonIDs []uuid.UUID) error {
  transaction := tx
  if transaction == nil {
    transaction = r.db
  }

  if len(lessonIDs) == 0 {
    return nil
  }

  if err := transaction.WithContext(ctx).
    Where("lesson_id IN ?", lessonIDs).
    Delete(&types.LessonAsset{}).Error; err != nil {
    return err
  }
  return nil
}

func (r *lessonAssetRepo) FullDeleteByIDs(ctx context.Context, tx *gorm.DB, assetIDs []uuid.UUID) error {
  transaction := tx
  if transaction == nil {
    transaction = r.db
  }

  if len(assetIDs) == 0 {
    return nil
  }

  if err := transaction.WithContext(ctx).
    Unscoped().
    Where("id IN ?", assetIDs).
    Delete(&types.LessonAsset{}).Error; err != nil {
    return err
  }
  return nil
}

func (r *lessonAssetRepo) FullDeleteByLessonIDs(ctx context.Context, tx *gorm.DB, lessonIDs []uuid.UUID) error {
  transaction := tx
  if transaction == nil {
    transaction = r.db
  }

  if len(lessonIDs) == 0 {
    return nil
  }

  if err := transaction.WithContext(ctx).
    Unscoped().
    Where("lesson_id IN ?", lessonIDs).
    Delete(&types.LessonAsset{}).Error; err != nil {
    return err
  }
  return nil
}










