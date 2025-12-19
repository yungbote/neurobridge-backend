package materials

import (
	"context"
	"github.com/google/uuid"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"gorm.io/gorm"
	"time"
)

type MaterialChunkRepo interface {
	Create(ctx context.Context, tx *gorm.DB, chunks []*types.MaterialChunk) ([]*types.MaterialChunk, error)
	GetByMaterialFileIDs(ctx context.Context, tx *gorm.DB, fileIDs []uuid.UUID) ([]*types.MaterialChunk, error)
	GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.MaterialChunk, error)
	UpdateFields(ctx context.Context, tx *gorm.DB, id uuid.UUID, updates map[string]interface{}) error
}

type materialChunkRepo struct {
	db  *gorm.DB
	log *logger.Logger
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

func (r *materialChunkRepo) UpdateFields(ctx context.Context, tx *gorm.DB, id uuid.UUID, updates map[string]interface{}) error {
	transaction := tx
	if transaction == nil {
		transaction = r.db
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
	return transaction.WithContext(ctx).
		Model(&types.MaterialChunk{}).
		Where("id = ?", id).
		Updates(updates).Error
}
