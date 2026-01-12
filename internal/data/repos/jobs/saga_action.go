package jobs

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type SagaActionRepo interface {
	Create(dbc dbctx.Context, rows []*types.SagaAction) ([]*types.SagaAction, error)

	GetByIDs(dbc dbctx.Context, ids []uuid.UUID) ([]*types.SagaAction, error)
	ListBySagaIDDesc(dbc dbctx.Context, sagaID uuid.UUID) ([]*types.SagaAction, error)

	GetMaxSeq(dbc dbctx.Context, sagaID uuid.UUID) (int64, error)

	UpdateFields(dbc dbctx.Context, id uuid.UUID, updates map[string]interface{}) error
}

type sagaActionRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewSagaActionRepo(db *gorm.DB, baseLog *logger.Logger) SagaActionRepo {
	return &sagaActionRepo{db: db, log: baseLog.With("repo", "SagaActionRepo")}
}

func (r *sagaActionRepo) Create(dbc dbctx.Context, rows []*types.SagaAction) ([]*types.SagaAction, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return []*types.SagaAction{}, nil
	}
	if err := t.WithContext(dbc.Ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *sagaActionRepo) GetByIDs(dbc dbctx.Context, ids []uuid.UUID) ([]*types.SagaAction, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.SagaAction
	if len(ids) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).Where("id IN ?", ids).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *sagaActionRepo) ListBySagaIDDesc(dbc dbctx.Context, sagaID uuid.UUID) ([]*types.SagaAction, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.SagaAction
	if sagaID == uuid.Nil {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("saga_id = ?", sagaID).
		Order("seq DESC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *sagaActionRepo) GetMaxSeq(dbc dbctx.Context, sagaID uuid.UUID) (int64, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if sagaID == uuid.Nil {
		return 0, nil
	}
	var max int64
	if err := t.WithContext(dbc.Ctx).
		Model(&types.SagaAction{}).
		Select("COALESCE(MAX(seq), 0)").
		Where("saga_id = ?", sagaID).
		Scan(&max).Error; err != nil {
		return 0, err
	}
	return max, nil
}

func (r *sagaActionRepo) UpdateFields(dbc dbctx.Context, id uuid.UUID, updates map[string]interface{}) error {
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
		Model(&types.SagaAction{}).
		Where("id = ?", id).
		Updates(updates).Error
}
