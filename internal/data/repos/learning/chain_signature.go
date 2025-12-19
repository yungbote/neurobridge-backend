package learning

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type ChainSignatureRepo interface {
	Create(ctx context.Context, tx *gorm.DB, rows []*types.ChainSignature) ([]*types.ChainSignature, error)
	GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.ChainSignature, error)
	GetByChainKeys(ctx context.Context, tx *gorm.DB, keys []string) ([]*types.ChainSignature, error)

	UpsertByChainKey(ctx context.Context, tx *gorm.DB, row *types.ChainSignature) error
	UpdateFields(ctx context.Context, tx *gorm.DB, id uuid.UUID, updates map[string]interface{}) error
}

type chainSignatureRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewChainSignatureRepo(db *gorm.DB, baseLog *logger.Logger) ChainSignatureRepo {
	return &chainSignatureRepo{db: db, log: baseLog.With("repo", "ChainSignatureRepo")}
}

func (r *chainSignatureRepo) Create(ctx context.Context, tx *gorm.DB, rows []*types.ChainSignature) ([]*types.ChainSignature, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return []*types.ChainSignature{}, nil
	}
	if err := t.WithContext(ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *chainSignatureRepo) GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.ChainSignature, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.ChainSignature
	if len(ids) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).Where("id IN ?", ids).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *chainSignatureRepo) GetByChainKeys(ctx context.Context, tx *gorm.DB, keys []string) ([]*types.ChainSignature, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.ChainSignature
	if len(keys) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).Where("chain_key IN ?", keys).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *chainSignatureRepo) UpsertByChainKey(ctx context.Context, tx *gorm.DB, row *types.ChainSignature) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if row == nil || row.ChainKey == "" {
		return nil
	}
	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	row.UpdatedAt = time.Now().UTC()

	return t.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "chain_key"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"scope", "scope_id", "concept_keys", "edges_json",
				"chain_doc", "embedding", "vector_id", "metadata",
				"updated_at",
			}),
		}).
		Create(row).Error
}

func (r *chainSignatureRepo) UpdateFields(ctx context.Context, tx *gorm.DB, id uuid.UUID, updates map[string]interface{}) error {
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
	return t.WithContext(ctx).Model(&types.ChainSignature{}).Where("id = ?", id).Updates(updates).Error
}










