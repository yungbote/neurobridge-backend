package repos

import (
  "context"
  "github.com/google/uuid"
  "gorm.io/gorm"
  "github.com/yungbote/neurobridge-backend/internal/logger"
  "github.com/yungbote/neurobridge-backend/internal/types"
)

type MaterialChunkRepo interface {
  Create(ctx context.Context, tx *gorm.DB, chunks []*types.MaterialChunk) ([]*types.MaterialChunk, error)
  GetByMaterialFileIDs(ctx context.Context, tx *gorm.DB, fileIDs []uuid.UUID) ([]*types.MaterialChunk, error)
  GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.MaterialChunk, error)
}

type materialChunkRepo struct {
  db          *gorm.DB
  log         *logger.Logger
}

func NewMaterialChunkRepo(db *gorm.DB, baseLog *logger.Logger) MaterialChunkRepo {
  repoLog := baseLog.With("repo", "MaterialChunkRepo")
  return &materialChunkRepo{db: db, log: repoLog}
}

func (r *materialChunkRepo) Create(ctx context.Context, tx *gorm.DB, chunks []*types.MaterialChunk) ([]*types.MaterialChunk, error) {
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}
	if len(chunks) == 0 {
		return []*types.MaterialChunk{}, nil
	}

	// Keep batches small because Text is large
	const batchSize = 100

	if err := transaction.WithContext(ctx).CreateInBatches(chunks, batchSize).Error; err != nil {
		return nil, err
	}
	return chunks, nil
}

func (r *materialChunkRepo) GetByMaterialFileIDs(ctx context.Context, tx *gorm.DB, fileIDs []uuid.UUID) ([]*types.MaterialChunk, error) {
  transaction := tx
  if transaction == nil {
    transaction = r.db
  }
  var results []*types.MaterialChunk
  if len(fileIDs) == 0 {
    return results, nil
  }
  if err := transaction.WithContext(ctx).
    Where("material_file_id IN ?", fileIDs).
    Order("material_file_id, index ASC").
    Find(&results).Error; err != nil {
    return nil, err
  }
  return results, nil
}

func (r *materialChunkRepo) GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.MaterialChunk, error) {
  transaction := tx
  if transaction == nil {
    transaction = r.db
  }
  var results []*types.MaterialChunk
  if len(ids) == 0 {
    return results, nil
  }
  if err := transaction.WithContext(ctx).
    Where("id IN ?", ids).
    Find(&results).Error; err != nil {
    return nil, err
  }
  return results, nil
}










