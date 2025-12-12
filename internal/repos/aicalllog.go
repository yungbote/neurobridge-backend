package repos

import (
  "context"
  "github.com/google/uuid"
  "gorm.io/gorm"
  "github.com/yungbote/neurobridge-backend/internal/logger"
  "github.com/yungbote/neurobridge-backend/internal/types"
)

type AICallLogRepo interface {
  Create(ctx context.Context, tx *gorm.DB, log []*types.AICallLog) ([]*types.AICallLog, error)
}

type aiCallLogRepo struct {
  db          *gorm.DB
  log         *logger.Logger
}

func NewAICallLogRepo(db *gorm.DB, baseLog *logger.Logger) AICallLogRepo {
  repoLog := baseLog.With("repo", "AICallLogRepo")
  return &aiCallLogRepo{db: db, log: repoLog}
}

func (r *aiCallLogRepo) Create(ctx context.Context, tx *gorm.DB, logs []*types.AICallLog) ([]*types.AICallLog, error) {
  transaction := tx
  if transaction == nil {
    transaction = r.db
  }
  if len(logs) == 0 {
    return []*types.AICallLog{}, nil
  }
  if err := transaction.WithContext(ctx).Create(&logs).Error; err != nil {
    return nil, err
  }
  return logs, nil
}










