package repos

import (
  "context"
  "errors"
  "time"
  "github.com/google/uuid"
  "gorm.io/gorm"
  "gorm.io/gorm/clause"
  "github.com/yungbote/neurobridge-backend/internal/logger"
  "github.com/yungbote/neurobridge-backend/internal/types"
)

type JobRunRepo interface {
  Create(ctx context.Context, tx *gorm.DB, jobs []*types.JobRun) ([]*types.JobRun, error)
  GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.JobRun, error)
  GetLatestByEntity(ctx context.Context, tx *gorm.DB, ownerUserID uuid.UUID, entityType string, entityID uuid.UUID, jobType string) (*types.JobRun, error)
  ClaimNextRunnable(ctx context.Context, tx *gorm.DB, maxAttempts int, retryDelay time.Duration, staleRunning time.Duration) (*types.JobRun, error)
  UpdateFields(ctx context.Context, tx *gorm.DB, id uuid.UUID, updates map[string]interface{}) error
  Heartbeat(ctx context.Context, tx *gorm.DB, id uuid.UUID) error
}

type jobRunRepo struct {
  db          *gorm.DB
  log         *logger.Logger
}

func NewJobRunRepo(db *gorm.DB, baseLog *logger.Logger) JobRunRepo {
  return &jobRunRepo{
    db:   db,
    log:  baseLog.With("repo", "JobRunRepo"),
  }
}

func (r *jobRunRepo) Create(ctx context.Context, tx *gorm.DB, jobs []*types.JobRun) ([]*types.JobRun, error) {
  transaction := tx
  if transaction == nil {
    transaction = r.db
  }
  if len(jobs) == 0 {
    return []*types.JobRun{}, nil
  }
  if err := transaction.WithContext(ctx).Create(&jobs).Error; err != nil {
    return nil, err
  }
  return jobs, nil
}

func (r *jobRunRepo) GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.JobRun, error) {
  transaction := tx
  if transaction == nil {
    transactiojn = r.db
  }
  var out []*types.JobRun
  if len(ids) == 0 {
    return out, nil
  }
  if err := transaction.WithContext(ctx).
    Where("id IN ?", ids).
    Find(&out).Error; err != nil {
    return nil, err
  }
  return out, nil
}

func (r *jobRunRepo) GetLatestByEntity(ctx context.Context, tx *gorm.DB, ownerUserID uuid.UUID, entityType string, entityID uuid.UUID, jobType string) (*types.JobRun, error) {
  transaction := tx
  if transaction == nil {
    transaction = r.db
  }
  if ownerUserID == uuid.Nil || entityID == uuid.Nil || entityType == "" || jobType == "" {
    return nil, nil
  }
  var job types.JobRun
  err := transaction.WithContext(ctx).
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

func (r *jobRunRepo) ClaimNextRunnable(ctx context.Context, tx *gorm.DB, maxAttempts int, retryDelay time.Duration, staleRunning time.Duration) (*types.JobRun, error) {
  transaction := tx
  if transaction == nil {
    transaction = r.db
  }
  now := time.Now()
  retryCutoff := now.Add(-retryDelay)
  staleCutoff := now.Add(-staleRunning)
  var claimed *types.JobRun
  err := transaction.WithContext(ctx).Transaction(func(txx *gorm.DB) error {
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

func (r *jobRunRepo) UpdateFields(ctx context.Context, tx *gorm.DB, id uuid.UUID, updates map[string]interface{}) error {
	transaction := tx
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
	return transaction.WithContext(ctx).
		Model(&types.JobRun{}).
		Where("id = ?", id).
		Updates(updates).Error
}


func (r *jobRunRepo) Heartbeat(ctx context.Context, tx *gorm.DB, id uuid.UUID) error {
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}
	if id == uuid.Nil {
		return nil
	}
	now := time.Now()
	return transaction.WithContext(ctx).
		Model(&types.JobRun{}).
		Where("id = ? AND status = ?", id, "running").
		Updates(map[string]interface{}{
			"heartbeat_at": now,
			"updated_at":   now,
		}).Error
}










