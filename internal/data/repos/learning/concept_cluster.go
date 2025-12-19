package learning

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type ConceptClusterRepo interface {
	Create(ctx context.Context, tx *gorm.DB, rows []*types.ConceptCluster) ([]*types.ConceptCluster, error)

	GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.ConceptCluster, error)
	GetByID(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*types.ConceptCluster, error)

	GetByScope(ctx context.Context, tx *gorm.DB, scope string, scopeID *uuid.UUID) ([]*types.ConceptCluster, error)

	UpdateFields(ctx context.Context, tx *gorm.DB, id uuid.UUID, updates map[string]interface{}) error

	SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
	SoftDeleteByScope(ctx context.Context, tx *gorm.DB, scope string, scopeID *uuid.UUID) error
	FullDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
	FullDeleteByScope(ctx context.Context, tx *gorm.DB, scope string, scopeID *uuid.UUID) error
}

type conceptClusterRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewConceptClusterRepo(db *gorm.DB, baseLog *logger.Logger) ConceptClusterRepo {
	return &conceptClusterRepo{db: db, log: baseLog.With("repo", "ConceptClusterRepo")}
}

func (r *conceptClusterRepo) Create(ctx context.Context, tx *gorm.DB, rows []*types.ConceptCluster) ([]*types.ConceptCluster, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return []*types.ConceptCluster{}, nil
	}
	if err := t.WithContext(ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *conceptClusterRepo) GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.ConceptCluster, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.ConceptCluster
	if len(ids) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).Where("id IN ?", ids).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *conceptClusterRepo) GetByID(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*types.ConceptCluster, error) {
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

func (r *conceptClusterRepo) GetByScope(ctx context.Context, tx *gorm.DB, scope string, scopeID *uuid.UUID) ([]*types.ConceptCluster, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.ConceptCluster
	if scope == "" {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("scope = ? AND scope_id IS NOT DISTINCT FROM ?", scope, scopeID).
		Order("created_at DESC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *conceptClusterRepo) UpdateFields(ctx context.Context, tx *gorm.DB, id uuid.UUID, updates map[string]interface{}) error {
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
		Model(&types.ConceptCluster{}).
		Where("id = ?", id).
		Updates(updates).Error
}

func (r *conceptClusterRepo) SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(ctx).Where("id IN ?", ids).Delete(&types.ConceptCluster{}).Error
}

func (r *conceptClusterRepo) SoftDeleteByScope(ctx context.Context, tx *gorm.DB, scope string, scopeID *uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if scope == "" {
		return nil
	}
	return t.WithContext(ctx).
		Where("scope = ? AND scope_id IS NOT DISTINCT FROM ?", scope, scopeID).
		Delete(&types.ConceptCluster{}).Error
}

func (r *conceptClusterRepo) FullDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(ctx).Unscoped().Where("id IN ?", ids).Delete(&types.ConceptCluster{}).Error
}

func (r *conceptClusterRepo) FullDeleteByScope(ctx context.Context, tx *gorm.DB, scope string, scopeID *uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if scope == "" {
		return nil
	}
	return t.WithContext(ctx).Unscoped().
		Where("scope = ? AND scope_id IS NOT DISTINCT FROM ?", scope, scopeID).
		Delete(&types.ConceptCluster{}).Error
}










