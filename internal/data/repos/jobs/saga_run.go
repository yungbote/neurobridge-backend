package jobs

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type SagaRunRepo interface {
	Create(ctx context.Context, tx *gorm.DB, rows []*types.SagaRun) ([]*types.SagaRun, error)

	GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.SagaRun, error)
	GetByID(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*types.SagaRun, error)
	GetByRootJobID(ctx context.Context, tx *gorm.DB, rootJobID uuid.UUID) (*types.SagaRun, error)

	LockByID(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*types.SagaRun, error)

	UpdateFields(ctx context.Context, tx *gorm.DB, id uuid.UUID, updates map[string]interface{}) error

	ListByStatusBefore(ctx context.Context, tx *gorm.DB, statuses []string, before time.Time, limit int) ([]*types.SagaRun, error)
}

type sagaRunRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewSagaRunRepo(db *gorm.DB, baseLog *logger.Logger) SagaRunRepo {
	return &sagaRunRepo{db: db, log: baseLog.With("repo", "SagaRunRepo")}
}

func (r *sagaRunRepo) Create(ctx context.Context, tx *gorm.DB, rows []*types.SagaRun) ([]*types.SagaRun, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return []*types.SagaRun{}, nil
	}
	if err := t.WithContext(ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *sagaRunRepo) GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.SagaRun, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.SagaRun
	if len(ids) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).Where("id IN ?", ids).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *sagaRunRepo) GetByID(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*types.SagaRun, error) {
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

func (r *sagaRunRepo) GetByRootJobID(ctx context.Context, tx *gorm.DB, rootJobID uuid.UUID) (*types.SagaRun, error) {
	if rootJobID == uuid.Nil {
		return nil, nil
	}
	t := tx
	if t == nil {
		t = r.db
	}
	var row types.SagaRun
	if err := t.WithContext(ctx).Where("root_job_id = ?", rootJobID).Limit(1).Find(&row).Error; err != nil {
		return nil, err
	}
	if row.ID == uuid.Nil {
		return nil, nil
	}
	return &row, nil
}

func (r *sagaRunRepo) LockByID(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*types.SagaRun, error) {
	if id == uuid.Nil {
		return nil, nil
	}
	t := tx
	if t == nil {
		t = r.db
	}
	var row types.SagaRun
	err := t.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ?", id).
		Limit(1).
		Find(&row).Error
	if err != nil {
		return nil, err
	}
	if row.ID == uuid.Nil {
		return nil, nil
	}
	return &row, nil
}

func (r *sagaRunRepo) UpdateFields(ctx context.Context, tx *gorm.DB, id uuid.UUID, updates map[string]interface{}) error {
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
		Model(&types.SagaRun{}).
		Where("id = ?", id).
		Updates(updates).Error
}

func (r *sagaRunRepo) ListByStatusBefore(ctx context.Context, tx *gorm.DB, statuses []string, before time.Time, limit int) ([]*types.SagaRun, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.SagaRun
	if len(statuses) == 0 {
		return out, nil
	}
	q := t.WithContext(ctx).Where("status IN ? AND updated_at < ?", statuses, before).Order("updated_at ASC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	if err := q.Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}
