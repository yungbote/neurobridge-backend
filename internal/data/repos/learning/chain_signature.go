package learning

import (
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type ChainSignatureRepo interface {
	Create(dbc dbctx.Context, rows []*types.ChainSignature) ([]*types.ChainSignature, error)
	GetByIDs(dbc dbctx.Context, ids []uuid.UUID) ([]*types.ChainSignature, error)
	GetByChainKeys(dbc dbctx.Context, keys []string) ([]*types.ChainSignature, error)
	ListByScope(dbc dbctx.Context, scope string, scopeID *uuid.UUID) ([]*types.ChainSignature, error)

	UpsertByChainKey(dbc dbctx.Context, row *types.ChainSignature) error
	UpdateFields(dbc dbctx.Context, id uuid.UUID, updates map[string]interface{}) error
}

type chainSignatureRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewChainSignatureRepo(db *gorm.DB, baseLog *logger.Logger) ChainSignatureRepo {
	return &chainSignatureRepo{db: db, log: baseLog.With("repo", "ChainSignatureRepo")}
}

func (r *chainSignatureRepo) Create(dbc dbctx.Context, rows []*types.ChainSignature) ([]*types.ChainSignature, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return []*types.ChainSignature{}, nil
	}
	if err := t.WithContext(dbc.Ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *chainSignatureRepo) GetByIDs(dbc dbctx.Context, ids []uuid.UUID) ([]*types.ChainSignature, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.ChainSignature
	if len(ids) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).Where("id IN ?", ids).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *chainSignatureRepo) GetByChainKeys(dbc dbctx.Context, keys []string) ([]*types.ChainSignature, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.ChainSignature
	if len(keys) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).Where("chain_key IN ?", keys).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *chainSignatureRepo) ListByScope(dbc dbctx.Context, scope string, scopeID *uuid.UUID) ([]*types.ChainSignature, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.ChainSignature
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("scope = ? AND scope_id IS NOT DISTINCT FROM ?", scope, scopeID).
		Order("chain_key ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *chainSignatureRepo) UpsertByChainKey(dbc dbctx.Context, row *types.ChainSignature) error {
	t := dbc.Tx
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

	return t.WithContext(dbc.Ctx).
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

func (r *chainSignatureRepo) UpdateFields(dbc dbctx.Context, id uuid.UUID, updates map[string]interface{}) error {
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
	return t.WithContext(dbc.Ctx).Model(&types.ChainSignature{}).Where("id = ?", id).Updates(updates).Error
}
