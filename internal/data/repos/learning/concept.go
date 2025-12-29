package learning

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
)

type ConceptRepo interface {
	Create(dbc dbctx.Context, rows []*types.Concept) ([]*types.Concept, error)

	GetByIDs(dbc dbctx.Context, ids []uuid.UUID) ([]*types.Concept, error)
	GetByID(dbc dbctx.Context, id uuid.UUID) (*types.Concept, error)

	GetByScope(dbc dbctx.Context, scope string, scopeID *uuid.UUID) ([]*types.Concept, error)
	GetByScopeAndKeys(dbc dbctx.Context, scope string, scopeID *uuid.UUID, keys []string) ([]*types.Concept, error)
	GetByScopeAndParent(dbc dbctx.Context, scope string, scopeID *uuid.UUID, parentID *uuid.UUID) ([]*types.Concept, error)
	GetByParentIDs(dbc dbctx.Context, parentIDs []uuid.UUID) ([]*types.Concept, error)
	GetByVectorIDs(dbc dbctx.Context, vectorIDs []string) ([]*types.Concept, error)

	UpsertByScopeAndKey(dbc dbctx.Context, row *types.Concept) error
	Update(dbc dbctx.Context, row *types.Concept) error
	UpdateFields(dbc dbctx.Context, id uuid.UUID, updates map[string]interface{}) error

	SoftDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error
	SoftDeleteByScope(dbc dbctx.Context, scope string, scopeID *uuid.UUID) error
	SoftDeleteByParentIDs(dbc dbctx.Context, parentIDs []uuid.UUID) error
	FullDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error
	FullDeleteByScope(dbc dbctx.Context, scope string, scopeID *uuid.UUID) error
	FullDeleteByParentIDs(dbc dbctx.Context, parentIDs []uuid.UUID) error
}

type conceptRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewConceptRepo(db *gorm.DB, baseLog *logger.Logger) ConceptRepo {
	return &conceptRepo{db: db, log: baseLog.With("repo", "ConceptRepo")}
}

func (r *conceptRepo) Create(dbc dbctx.Context, rows []*types.Concept) ([]*types.Concept, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return []*types.Concept{}, nil
	}
	if err := t.WithContext(dbc.Ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *conceptRepo) GetByIDs(dbc dbctx.Context, ids []uuid.UUID) ([]*types.Concept, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.Concept
	if len(ids) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).Where("id IN ?", ids).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *conceptRepo) GetByID(dbc dbctx.Context, id uuid.UUID) (*types.Concept, error) {
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

func (r *conceptRepo) GetByScope(dbc dbctx.Context, scope string, scopeID *uuid.UUID) ([]*types.Concept, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.Concept
	if scope == "" {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("scope = ? AND scope_id IS NOT DISTINCT FROM ?", scope, scopeID).
		Order("depth ASC, sort_index ASC, key ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *conceptRepo) GetByScopeAndKeys(dbc dbctx.Context, scope string, scopeID *uuid.UUID, keys []string) ([]*types.Concept, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.Concept
	if scope == "" || len(keys) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("scope = ? AND scope_id IS NOT DISTINCT FROM ? AND key IN ?", scope, scopeID, keys).
		Order("depth ASC, sort_index ASC, key ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *conceptRepo) GetByScopeAndParent(dbc dbctx.Context, scope string, scopeID *uuid.UUID, parentID *uuid.UUID) ([]*types.Concept, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.Concept
	if scope == "" {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("scope = ? AND scope_id IS NOT DISTINCT FROM ? AND parent_id IS NOT DISTINCT FROM ?", scope, scopeID, parentID).
		Order("sort_index ASC, key ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *conceptRepo) GetByParentIDs(dbc dbctx.Context, parentIDs []uuid.UUID) ([]*types.Concept, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.Concept
	if len(parentIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("parent_id IN ?", parentIDs).
		Order("parent_id ASC, sort_index ASC, key ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *conceptRepo) GetByVectorIDs(dbc dbctx.Context, vectorIDs []string) ([]*types.Concept, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.Concept
	if len(vectorIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("vector_id IN ?", vectorIDs).
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *conceptRepo) UpsertByScopeAndKey(dbc dbctx.Context, row *types.Concept) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if row == nil || row.Scope == "" || row.Key == "" {
		return nil
	}
	row.UpdatedAt = time.Now().UTC()

	// Upsert by (scope, scope_id, key). We use a find+assign to avoid relying on a specific unique index.
	return t.WithContext(dbc.Ctx).
		Where("scope = ? AND scope_id IS NOT DISTINCT FROM ? AND key = ?", row.Scope, row.ScopeID, row.Key).
		Assign(row).
		FirstOrCreate(row).Error
}

func (r *conceptRepo) Update(dbc dbctx.Context, row *types.Concept) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if row == nil {
		return nil
	}
	return t.WithContext(dbc.Ctx).Save(row).Error
}

func (r *conceptRepo) UpdateFields(dbc dbctx.Context, id uuid.UUID, updates map[string]interface{}) error {
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
		Model(&types.Concept{}).
		Where("id = ?", id).
		Updates(updates).Error
}

func (r *conceptRepo) SoftDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Where("id IN ?", ids).Delete(&types.Concept{}).Error
}

func (r *conceptRepo) SoftDeleteByScope(dbc dbctx.Context, scope string, scopeID *uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if scope == "" {
		return nil
	}
	return t.WithContext(dbc.Ctx).
		Where("scope = ? AND scope_id IS NOT DISTINCT FROM ?", scope, scopeID).
		Delete(&types.Concept{}).Error
}

func (r *conceptRepo) SoftDeleteByParentIDs(dbc dbctx.Context, parentIDs []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(parentIDs) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Where("parent_id IN ?", parentIDs).Delete(&types.Concept{}).Error
}

func (r *conceptRepo) FullDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Unscoped().Where("id IN ?", ids).Delete(&types.Concept{}).Error
}

func (r *conceptRepo) FullDeleteByScope(dbc dbctx.Context, scope string, scopeID *uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if scope == "" {
		return nil
	}
	return t.WithContext(dbc.Ctx).
		Unscoped().
		Where("scope = ? AND scope_id IS NOT DISTINCT FROM ?", scope, scopeID).
		Delete(&types.Concept{}).Error
}

func (r *conceptRepo) FullDeleteByParentIDs(dbc dbctx.Context, parentIDs []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(parentIDs) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Unscoped().Where("parent_id IN ?", parentIDs).Delete(&types.Concept{}).Error
}
