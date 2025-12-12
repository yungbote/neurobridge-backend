package repos

import (
  "context"
  "github.com/google/uuid"
  "gorm.io/gorm"
  "github.com/yungbote/neurobridge-backend/internal/logger"
  "github.com/yungbote/neurobridge-backend/internal/types"
)

type UserEventRepo interface {
  Create(ctx context.Context, tx *gorm.DB, events []*types.UserEvent) ([]*types.UserEvent, error)
  GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.UserEvent, error)
  GetByUserID(ctx context.Context, tx *gorm.DB, userID uuid.UUID) ([]*types.UserEvent, error)
  GetByUserAndCourseID(ctx context.Context, tx *gorm.DB, userID, courseID uuid.UUID) ([]*types.UserEvent, error)
  SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
  FullDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
}

type userEventRepo struct {
  db  *gorm.DB
  log *logger.Logger
}

func NewUserEventRepo(db *gorm.DB, baseLog *logger.Logger) UserEventRepo {
  repoLog := baseLog.With("repo", "UserEventRepo")
  return &userEventRepo{db: db, log: repoLog}
}

func (r *userEventRepo) Create(ctx context.Context, tx *gorm.DB, events []*types.UserEvent) ([]*types.UserEvent, error) {
  transaction := tx
  if transaction == nil {
    transaction = r.db
  }

  if len(events) == 0 {
    return []*types.UserEvent{}, nil
  }

  if err := transaction.WithContext(ctx).Create(&events).Error; err != nil {
    return nil, err
  }
  return events, nil
}

func (r *userEventRepo) GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.UserEvent, error) {
  transaction := tx
  if transaction == nil {
    transaction = r.db
  }

  var results []*types.UserEvent
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

func (r *userEventRepo) GetByUserID(ctx context.Context, tx *gorm.DB, userID uuid.UUID) ([]*types.UserEvent, error) {
  transaction := tx
  if transaction == nil {
    transaction = r.db
  }

  var results []*types.UserEvent
  if userID == uuid.Nil {
    return results, nil
  }

  if err := transaction.WithContext(ctx).
    Where("user_id = ?", userID).
    Order("created_at DESC").
    Find(&results).Error; err != nil {
    return nil, err
  }
  return results, nil
}

func (r *userEventRepo) GetByUserAndCourseID(ctx context.Context, tx *gorm.DB, userID, courseID uuid.UUID) ([]*types.UserEvent, error) {
  transaction := tx
  if transaction == nil {
    transaction = r.db
  }

  var results []*types.UserEvent
  if userID == uuid.Nil || courseID == uuid.Nil {
    return results, nil
  }

  if err := transaction.WithContext(ctx).
    Where("user_id = ? AND course_id = ?", userID, courseID).
    Order("created_at DESC").
    Find(&results).Error; err != nil {
    return nil, err
  }
  return results, nil
}

func (r *userEventRepo) SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error {
  transaction := tx
  if transaction == nil {
    transaction = r.db
  }

  if len(ids) == 0 {
    return nil
  }

  if err := transaction.WithContext(ctx).
    Where("id IN ?", ids).
    Delete(&types.UserEvent{}).Error; err != nil {
    return err
  }
  return nil
}

func (r *userEventRepo) FullDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error {
  transaction := tx
  if transaction == nil {
    transaction = r.db
  }

  if len(ids) == 0 {
    return nil
  }

  if err := transaction.WithContext(ctx).
    Unscoped().
    Where("id IN ?", ids).
    Delete(&types.UserEvent{}).Error; err != nil {
    return err
  }
  return nil
}










