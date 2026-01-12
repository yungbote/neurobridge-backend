package learning

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type DecisionTraceRepo interface {
	Create(dbc dbctx.Context, rows []*types.DecisionTrace) ([]*types.DecisionTrace, error)

	GetByIDs(dbc dbctx.Context, ids []uuid.UUID) ([]*types.DecisionTrace, error)
	ListByUser(dbc dbctx.Context, userID uuid.UUID, limit int) ([]*types.DecisionTrace, error)
}

type decisionTraceRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewDecisionTraceRepo(db *gorm.DB, baseLog *logger.Logger) DecisionTraceRepo {
	return &decisionTraceRepo{db: db, log: baseLog.With("repo", "DecisionTraceRepo")}
}

func (r *decisionTraceRepo) Create(dbc dbctx.Context, rows []*types.DecisionTrace) ([]*types.DecisionTrace, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return []*types.DecisionTrace{}, nil
	}
	if err := t.WithContext(dbc.Ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *decisionTraceRepo) GetByIDs(dbc dbctx.Context, ids []uuid.UUID) ([]*types.DecisionTrace, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.DecisionTrace
	if len(ids) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).Where("id IN ?", ids).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *decisionTraceRepo) ListByUser(dbc dbctx.Context, userID uuid.UUID, limit int) ([]*types.DecisionTrace, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.DecisionTrace
	if userID == uuid.Nil {
		return out, nil
	}
	if limit <= 0 {
		limit = 200
	}
	if limit > 2000 {
		limit = 2000
	}
	if err := t.WithContext(dbc.Ctx).
		Where("user_id = ?", userID).
		Order("occurred_at DESC, created_at DESC").
		Limit(limit).
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}
