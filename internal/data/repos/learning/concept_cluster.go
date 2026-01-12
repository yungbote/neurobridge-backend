package learning

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type ConceptClusterRepo interface {
	Create(dbc dbctx.Context, rows []*types.ConceptCluster) ([]*types.ConceptCluster, error)

	GetByIDs(dbc dbctx.Context, ids []uuid.UUID) ([]*types.ConceptCluster, error)
	GetByID(dbc dbctx.Context, id uuid.UUID) (*types.ConceptCluster, error)

	GetByScope(dbc dbctx.Context, scope string, scopeID *uuid.UUID) ([]*types.ConceptCluster, error)

	UpdateFields(dbc dbctx.Context, id uuid.UUID, updates map[string]interface{}) error

	SoftDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error
	SoftDeleteByScope(dbc dbctx.Context, scope string, scopeID *uuid.UUID) error
	FullDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error
	FullDeleteByScope(dbc dbctx.Context, scope string, scopeID *uuid.UUID) error
}

type conceptClusterRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewConceptClusterRepo(db *gorm.DB, baseLog *logger.Logger) ConceptClusterRepo {
	return &conceptClusterRepo{db: db, log: baseLog.With("repo", "ConceptClusterRepo")}
}

func (r *conceptClusterRepo) Create(dbc dbctx.Context, rows []*types.ConceptCluster) ([]*types.ConceptCluster, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return []*types.ConceptCluster{}, nil
	}
	if err := t.WithContext(dbc.Ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *conceptClusterRepo) GetByIDs(dbc dbctx.Context, ids []uuid.UUID) ([]*types.ConceptCluster, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.ConceptCluster
	if len(ids) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).Where("id IN ?", ids).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *conceptClusterRepo) GetByID(dbc dbctx.Context, id uuid.UUID) (*types.ConceptCluster, error) {
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

func (r *conceptClusterRepo) GetByScope(dbc dbctx.Context, scope string, scopeID *uuid.UUID) ([]*types.ConceptCluster, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.ConceptCluster
	if scope == "" {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("scope = ? AND scope_id IS NOT DISTINCT FROM ?", scope, scopeID).
		Order("created_at DESC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *conceptClusterRepo) UpdateFields(dbc dbctx.Context, id uuid.UUID, updates map[string]interface{}) error {
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
		Model(&types.ConceptCluster{}).
		Where("id = ?", id).
		Updates(updates).Error
}

func (r *conceptClusterRepo) SoftDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Where("id IN ?", ids).Delete(&types.ConceptCluster{}).Error
}

func (r *conceptClusterRepo) SoftDeleteByScope(dbc dbctx.Context, scope string, scopeID *uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if scope == "" {
		return nil
	}
	return t.WithContext(dbc.Ctx).
		Where("scope = ? AND scope_id IS NOT DISTINCT FROM ?", scope, scopeID).
		Delete(&types.ConceptCluster{}).Error
}

func (r *conceptClusterRepo) FullDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Unscoped().Where("id IN ?", ids).Delete(&types.ConceptCluster{}).Error
}

func (r *conceptClusterRepo) FullDeleteByScope(dbc dbctx.Context, scope string, scopeID *uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if scope == "" {
		return nil
	}
	return t.WithContext(dbc.Ctx).Unscoped().
		Where("scope = ? AND scope_id IS NOT DISTINCT FROM ?", scope, scopeID).
		Delete(&types.ConceptCluster{}).Error
}
