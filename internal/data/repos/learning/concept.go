package learning

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type ConceptRepo interface {
	Create(ctx context.Context, tx *gorm.DB, rows []*types.Concept) ([]*types.Concept, error)

	GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.Concept, error)
	GetByID(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*types.Concept, error)

	GetByScope(ctx context.Context, tx *gorm.DB, scope string, scopeID *uuid.UUID) ([]*types.Concept, error)
	GetByScopeAndKeys(ctx context.Context, tx *gorm.DB, scope string, scopeID *uuid.UUID, keys []string) ([]*types.Concept, error)
	GetByScopeAndParent(ctx context.Context, tx *gorm.DB, scope string, scopeID *uuid.UUID, parentID *uuid.UUID) ([]*types.Concept, error)
	GetByParentIDs(ctx context.Context, tx *gorm.DB, parentIDs []uuid.UUID) ([]*types.Concept, error)
	GetByVectorIDs(ctx context.Context, tx *gorm.DB, vectorIDs []string) ([]*types.Concept, error)

	UpsertByScopeAndKey(ctx context.Context, tx *gorm.DB, row *types.Concept) error
	Update(ctx context.Context, tx *gorm.DB, row *types.Concept) error
	UpdateFields(ctx context.Context, tx *gorm.DB, id uuid.UUID, updates map[string]interface{}) error

	SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
	SoftDeleteByScope(ctx context.Context, tx *gorm.DB, scope string, scopeID *uuid.UUID) error
	SoftDeleteByParentIDs(ctx context.Context, tx *gorm.DB, parentIDs []uuid.UUID) error
	FullDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
	FullDeleteByScope(ctx context.Context, tx *gorm.DB, scope string, scopeID *uuid.UUID) error
	FullDeleteByParentIDs(ctx context.Context, tx *gorm.DB, parentIDs []uuid.UUID) error
}

type conceptRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewConceptRepo(db *gorm.DB, baseLog *logger.Logger) ConceptRepo {
	return &conceptRepo{db: db, log: baseLog.With("repo", "ConceptRepo")}
}

func (r *conceptRepo) Create(ctx context.Context, tx *gorm.DB, rows []*types.Concept) ([]*types.Concept, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return []*types.Concept{}, nil
	}
	if err := t.WithContext(ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *conceptRepo) GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.Concept, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.Concept
	if len(ids) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).Where("id IN ?", ids).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *conceptRepo) GetByID(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*types.Concept, error) {
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

func (r *conceptRepo) GetByScope(ctx context.Context, tx *gorm.DB, scope string, scopeID *uuid.UUID) ([]*types.Concept, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.Concept
	if scope == "" {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("scope = ? AND scope_id IS NOT DISTINCT FROM ?", scope, scopeID).
		Order("depth ASC, sort_index ASC, key ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *conceptRepo) GetByScopeAndKeys(ctx context.Context, tx *gorm.DB, scope string, scopeID *uuid.UUID, keys []string) ([]*types.Concept, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.Concept
	if scope == "" || len(keys) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("scope = ? AND scope_id IS NOT DISTINCT FROM ? AND key IN ?", scope, scopeID, keys).
		Order("depth ASC, sort_index ASC, key ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *conceptRepo) GetByScopeAndParent(ctx context.Context, tx *gorm.DB, scope string, scopeID *uuid.UUID, parentID *uuid.UUID) ([]*types.Concept, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.Concept
	if scope == "" {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("scope = ? AND scope_id IS NOT DISTINCT FROM ? AND parent_id IS NOT DISTINCT FROM ?", scope, scopeID, parentID).
		Order("sort_index ASC, key ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *conceptRepo) GetByParentIDs(ctx context.Context, tx *gorm.DB, parentIDs []uuid.UUID) ([]*types.Concept, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.Concept
	if len(parentIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("parent_id IN ?", parentIDs).
		Order("parent_id ASC, sort_index ASC, key ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *conceptRepo) GetByVectorIDs(ctx context.Context, tx *gorm.DB, vectorIDs []string) ([]*types.Concept, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.Concept
	if len(vectorIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("vector_id IN ?", vectorIDs).
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *conceptRepo) UpsertByScopeAndKey(ctx context.Context, tx *gorm.DB, row *types.Concept) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if row == nil || row.Scope == "" || row.Key == "" {
		return nil
	}
	row.UpdatedAt = time.Now().UTC()

	// Upsert by (scope, scope_id, key). We use a find+assign to avoid relying on a specific unique index.
	return t.WithContext(ctx).
		Where("scope = ? AND scope_id IS NOT DISTINCT FROM ? AND key = ?", row.Scope, row.ScopeID, row.Key).
		Assign(row).
		FirstOrCreate(row).Error
}

func (r *conceptRepo) Update(ctx context.Context, tx *gorm.DB, row *types.Concept) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if row == nil {
		return nil
	}
	return t.WithContext(ctx).Save(row).Error
}

func (r *conceptRepo) UpdateFields(ctx context.Context, tx *gorm.DB, id uuid.UUID, updates map[string]interface{}) error {
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
		Model(&types.Concept{}).
		Where("id = ?", id).
		Updates(updates).Error
}

func (r *conceptRepo) SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(ctx).Where("id IN ?", ids).Delete(&types.Concept{}).Error
}

func (r *conceptRepo) SoftDeleteByScope(ctx context.Context, tx *gorm.DB, scope string, scopeID *uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if scope == "" {
		return nil
	}
	return t.WithContext(ctx).
		Where("scope = ? AND scope_id IS NOT DISTINCT FROM ?", scope, scopeID).
		Delete(&types.Concept{}).Error
}

func (r *conceptRepo) SoftDeleteByParentIDs(ctx context.Context, tx *gorm.DB, parentIDs []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(parentIDs) == 0 {
		return nil
	}
	return t.WithContext(ctx).Where("parent_id IN ?", parentIDs).Delete(&types.Concept{}).Error
}

func (r *conceptRepo) FullDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(ctx).Unscoped().Where("id IN ?", ids).Delete(&types.Concept{}).Error
}

func (r *conceptRepo) FullDeleteByScope(ctx context.Context, tx *gorm.DB, scope string, scopeID *uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if scope == "" {
		return nil
	}
	return t.WithContext(ctx).
		Unscoped().
		Where("scope = ? AND scope_id IS NOT DISTINCT FROM ?", scope, scopeID).
		Delete(&types.Concept{}).Error
}

func (r *conceptRepo) FullDeleteByParentIDs(ctx context.Context, tx *gorm.DB, parentIDs []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(parentIDs) == 0 {
		return nil
	}
	return t.WithContext(ctx).Unscoped().Where("parent_id IN ?", parentIDs).Delete(&types.Concept{}).Error
}










