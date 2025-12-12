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

type CourseGenerationRunRepo interface {
  Create(ctx context.Context, tx *gorm.DB, runs []*types.CourseGenerationRun) ([]*types.CourseGenerationRun, error)
  GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.CourseGenerationRun, error)

  // NEW: latest run for a course (used by /api/courses/:id/generation)
  GetLatestByCourseID(ctx context.Context, tx *gorm.DB, courseID uuid.UUID) (*types.CourseGenerationRun, error)

  // Claims the next run that is runnable:
  // - status=queued
  // - OR status=failed and attempts < maxAttempts and last_error_at older than retryDelay (or NULL)
  // - OR status=running but heartbeat is stale (crash recovery)
  ClaimNextRunnable(ctx context.Context, tx *gorm.DB, maxAttempts int, retryDelay time.Duration, staleRunning time.Duration) (*types.CourseGenerationRun, error)

  UpdateFields(ctx context.Context, tx *gorm.DB, id uuid.UUID, updates map[string]interface{}) error
  Heartbeat(ctx context.Context, tx *gorm.DB, id uuid.UUID) error
}

type courseGenerationRunRepo struct {
  db  *gorm.DB
  log *logger.Logger
}

func NewCourseGenerationRunRepo(db *gorm.DB, baseLog *logger.Logger) CourseGenerationRunRepo {
  repoLog := baseLog.With("repo", "CourseGenerationRunRepo")
  return &courseGenerationRunRepo{db: db, log: repoLog}
}

func (r *courseGenerationRunRepo) Create(ctx context.Context, tx *gorm.DB, runs []*types.CourseGenerationRun) ([]*types.CourseGenerationRun, error) {
  transaction := tx
  if transaction == nil {
    transaction = r.db
  }
  if len(runs) == 0 {
    return []*types.CourseGenerationRun{}, nil
  }
  if err := transaction.WithContext(ctx).Create(&runs).Error; err != nil {
    return nil, err
  }
  return runs, nil
}

func (r *courseGenerationRunRepo) GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.CourseGenerationRun, error) {
  transaction := tx
  if transaction == nil {
    transaction = r.db
  }
  var results []*types.CourseGenerationRun
  if len(ids) == 0 {
    return results, nil
  }
  if err := transaction.WithContext(ctx).
    Where("id IN ?", ids).
    Find(&results).Error; err != nil {
    return nil, err
  }
  return results, nil
}

// NEW: latest run for a course
func (r *courseGenerationRunRepo) GetLatestByCourseID(ctx context.Context, tx *gorm.DB, courseID uuid.UUID) (*types.CourseGenerationRun, error) {
  transaction := tx
  if transaction == nil {
    transaction = r.db
  }
  if courseID == uuid.Nil {
    return nil, nil
  }

  var run types.CourseGenerationRun
  err := transaction.WithContext(ctx).
    Where("course_id = ?", courseID).
    Order("created_at DESC").
    Limit(1).
    Find(&run).Error
  if err != nil {
    return nil, err
  }
  if run.ID == uuid.Nil {
    return nil, nil
  }
  return &run, nil
}

func (r *courseGenerationRunRepo) ClaimNextRunnable(
  ctx context.Context,
  tx *gorm.DB,
  maxAttempts int,
  retryDelay time.Duration,
  staleRunning time.Duration,
) (*types.CourseGenerationRun, error) {
  transaction := tx
  if transaction == nil {
    transaction = r.db
  }

  now := time.Now()
  retryCutoff := now.Add(-retryDelay)
  staleCutoff := now.Add(-staleRunning)

  var claimed *types.CourseGenerationRun

  err := transaction.WithContext(ctx).Transaction(func(txx *gorm.DB) error {
    var run types.CourseGenerationRun

    q := txx.
      Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
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

    qErr := q.First(&run).Error
    if errors.Is(qErr, gorm.ErrRecordNotFound) {
      return nil
    }
    if qErr != nil {
      return qErr
    }

    // Claim it: mark running, increment attempts, set lock/heartbeat.
    uErr := txx.Model(&types.CourseGenerationRun{}).
      Where("id = ?", run.ID).
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

    claimed = &run
    return nil
  })

  if err != nil {
    return nil, err
  }
  return claimed, nil
}

func (r *courseGenerationRunRepo) UpdateFields(ctx context.Context, tx *gorm.DB, id uuid.UUID, updates map[string]interface{}) error {
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
    Model(&types.CourseGenerationRun{}).
    Where("id = ?", id).
    Updates(updates).Error
}

func (r *courseGenerationRunRepo) Heartbeat(ctx context.Context, tx *gorm.DB, id uuid.UUID) error {
  transaction := tx
  if transaction == nil {
    transaction = r.db
  }
  if id == uuid.Nil {
    return nil
  }
  now := time.Now()
  return transaction.WithContext(ctx).
    Model(&types.CourseGenerationRun{}).
    Where("id = ? AND status = ?", id, "running").
    Updates(map[string]interface{}{
      "heartbeat_at": now,
      "updated_at":   now,
    }).Error
}










