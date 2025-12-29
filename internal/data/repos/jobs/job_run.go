package jobs

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type JobRunRepo interface {
	Create(dbc dbctx.Context, jobs []*types.JobRun) ([]*types.JobRun, error)
	GetByIDs(dbc dbctx.Context, ids []uuid.UUID) ([]*types.JobRun, error)
	GetLatestByEntity(dbc dbctx.Context, ownerUserID uuid.UUID, entityType string, entityID uuid.UUID, jobType string) (*types.JobRun, error)
	ClaimNextRunnable(dbc dbctx.Context, maxAttempts int, retryDelay time.Duration, staleRunning time.Duration) (*types.JobRun, error)
	UpdateFields(dbc dbctx.Context, id uuid.UUID, updates map[string]interface{}) error
	UpdateFieldsUnlessStatus(dbc dbctx.Context, id uuid.UUID, disallowedStatuses []string, updates map[string]interface{}) (bool, error)
	Heartbeat(dbc dbctx.Context, id uuid.UUID) error
	HasRunnableForEntity(dbc dbctx.Context, ownerUserID uuid.UUID, entityType string, entityID uuid.UUID, jobType string) (bool, error)
	ExistsRunnable(dbc dbctx.Context, ownerUserID uuid.UUID, jobType string, entityType string, entityID *uuid.UUID) (bool, error)
}

type jobRunRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewJobRunRepo(db *gorm.DB, baseLog *logger.Logger) JobRunRepo {
	return &jobRunRepo{
		db:  db,
		log: baseLog.With("repo", "JobRunRepo"),
	}
}

func (r *jobRunRepo) Create(dbc dbctx.Context, jobs []*types.JobRun) ([]*types.JobRun, error) {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}
	if len(jobs) == 0 {
		return []*types.JobRun{}, nil
	}
	if err := transaction.WithContext(dbc.Ctx).Create(&jobs).Error; err != nil {
		return nil, err
	}
	return jobs, nil
}

func (r *jobRunRepo) GetByIDs(dbc dbctx.Context, ids []uuid.UUID) ([]*types.JobRun, error) {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}
	var out []*types.JobRun
	if len(ids) == 0 {
		return out, nil
	}
	if err := transaction.WithContext(dbc.Ctx).
		Where("id IN ?", ids).
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *jobRunRepo) GetLatestByEntity(dbc dbctx.Context, ownerUserID uuid.UUID, entityType string, entityID uuid.UUID, jobType string) (*types.JobRun, error) {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}
	if ownerUserID == uuid.Nil || entityID == uuid.Nil || entityType == "" || jobType == "" {
		return nil, nil
	}
	var job types.JobRun
	err := transaction.WithContext(dbc.Ctx).
		Where("owner_user_id = ? AND entity_type = ? AND entity_id = ? AND job_type = ?", ownerUserID, entityType, entityID, jobType).
		Order("created_at DESC").
		Limit(1).
		Find(&job).Error
	if err != nil {
		return nil, err
	}
	if job.ID == uuid.Nil {
		return nil, nil
	}
	return &job, nil
}

func (r *jobRunRepo) ClaimNextRunnable(dbc dbctx.Context, maxAttempts int, retryDelay time.Duration, staleRunning time.Duration) (*types.JobRun, error) {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}
	now := time.Now()
	retryCutoff := now.Add(-retryDelay)
	staleCutoff := now.Add(-staleRunning)
	var claimed *types.JobRun
	err := transaction.WithContext(dbc.Ctx).Transaction(func(txx *gorm.DB) error {
		var job types.JobRun
		q := txx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where(`
        (
          status = ?
          OR (
            status = ?
            AND attempts < ?
            AND (last_error_at IS NULL OR last_error_at < ?)
          )
          OR (
            status = ?
            AND heartbeat_at IS NOT NULL
            AND heartbeat_at < ?
          )
        )
      `, "queued", "failed", maxAttempts, retryCutoff, "running", staleCutoff).
			Order("created_at ASC")
		qErr := q.First(&job).Error
		if errors.Is(qErr, gorm.ErrRecordNotFound) {
			return nil
		}
		if qErr != nil {
			return qErr
		}
		uErr := txx.Model(&types.JobRun{}).
			Where("id = ?", job.ID).
			Updates(map[string]interface{}{
				"status":       "running",
				"attempts":     gorm.Expr("attempts + 1"),
				"locked_at":    now,
				"heartbeat_at": now,
				"updated_at":   now,
			}).Error
		if uErr != nil {
			return uErr
		}
		claimed = &job
		return nil
	})
	if err != nil {
		return nil, err
	}
	return claimed, nil
}

func (r *jobRunRepo) UpdateFields(dbc dbctx.Context, id uuid.UUID, updates map[string]interface{}) error {
	transaction := dbc.Tx
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
		updates["updated_at"] = time.Now()
	}
	return transaction.WithContext(dbc.Ctx).
		Model(&types.JobRun{}).
		Where("id = ?", id).
		Updates(updates).Error
}

func (r *jobRunRepo) UpdateFieldsUnlessStatus(dbc dbctx.Context, id uuid.UUID, disallowedStatuses []string, updates map[string]interface{}) (bool, error) {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}
	if id == uuid.Nil {
		return false, nil
	}
	if updates == nil {
		updates = map[string]interface{}{}
	}
	if _, ok := updates["updated_at"]; !ok {
		updates["updated_at"] = time.Now()
	}

	q := transaction.WithContext(dbc.Ctx).
		Model(&types.JobRun{}).
		Where("id = ?", id)
	if len(disallowedStatuses) == 1 {
		q = q.Where("status <> ?", disallowedStatuses[0])
	} else if len(disallowedStatuses) > 1 {
		q = q.Where("status NOT IN ?", disallowedStatuses)
	}

	res := q.Updates(updates)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (r *jobRunRepo) Heartbeat(dbc dbctx.Context, id uuid.UUID) error {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}
	if id == uuid.Nil {
		return nil
	}
	now := time.Now()
	return transaction.WithContext(dbc.Ctx).
		Model(&types.JobRun{}).
		Where("id = ? AND status = ?", id, "running").
		Updates(map[string]interface{}{
			"heartbeat_at": now,
			"updated_at":   now,
		}).Error
}

func (r *jobRunRepo) HasRunnableForEntity(dbc dbctx.Context, ownerUserID uuid.UUID, entityType string, entityID uuid.UUID, jobType string) (bool, error) {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}
	if ownerUserID == uuid.Nil || entityID == uuid.Nil || entityType == "" || jobType == "" {
		return false, nil
	}
	var count int64
	err := transaction.WithContext(dbc.Ctx).
		Model(&types.JobRun{}).
		Where("owner_user_id = ? AND entity_type = ? AND entity_id = ? AND job_type = ? AND status IN ?",
			ownerUserID, entityType, entityID, jobType, []string{"queued", "running"},
		).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *jobRunRepo) ExistsRunnable(dbc dbctx.Context, ownerUserID uuid.UUID, jobType string, entityType string, entityID *uuid.UUID) (bool, error) {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}
	if ownerUserID == uuid.Nil || jobType == "" {
		return false, nil
	}

	q := transaction.WithContext(dbc.Ctx).Model(&types.JobRun{}).
		Where("owner_user_id = ? AND job_type = ? AND status IN ?", ownerUserID, jobType, []string{"queued", "running"})

	if entityType != "" {
		q = q.Where("entity_type = ?", entityType)
	}
	if entityID != nil && *entityID != uuid.Nil {
		q = q.Where("entity_id = ?", *entityID)
	}

	var count int64
	if err := q.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}
