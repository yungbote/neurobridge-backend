package repos

import (
  "context"
  "github.com/google/uuid"
  "gorm.io/gorm"
  "github.com/yungbote/neurobridge-backend/internal/logger"
  "github.com/yungbote/neurobridge-backend/internal/types"
)

type UserRepo interface {
  Create(ctx context.Context, tx *gorm.DB, users []*types.User) ([]*types.User, error)
  GetByIDs(ctx context.Context, tx *gorm.DB, userIDs []uuid.UUID) ([]*types.User, error)
  GetByEmails(ctx context.Context, tx *gorm.DB, userEmails []string) ([]*types.User, error)
  EmailExists(ctx context.Context, tx *gorm.DB, userEmail string) (bool, error)
}

type userRepo struct {
  db  *gorm.DB
  log *logger.Logger
}

func NewUserRepo(db *gorm.DB, baseLog *logger.Logger) UserRepo {
  repoLog := baseLog.With("repo", "UserRepo")
  return &userRepo{db: db, log: repoLog}
}

func (ur *userRepo) Create(ctx context.Context, tx *gorm.DB, users []*types.User) ([]*types.User, error) {
  transaction := tx
  if transaction == nil {
    transaction = ur.db
  }

  if len(users) == 0 {
    return []*types.User{}, nil
  }

  if err := transaction.WithContext(ctx).Create(&users).Error; err != nil {
    return nil, err
  }

  return users, nil
}

func (ur *userRepo) GetByIDs(ctx context.Context, tx *gorm.DB, userIDs []uuid.UUID) ([]*types.User, error) {
  transaction := tx
  if transaction == nil {
    transaction = ur.db
  }

  var results []*types.User

  if len(userIDs) == 0 {
    return results, nil
  }

  if err := transaction.WithContext(ctx).
    Where("id IN ?", userIDs).
    Find(&results).Error; err != nil {
      return nil, err
  }
  return results, nil
}

func (ur *userRepo) GetByEmails(ctx context.Context, tx *gorm.DB, userEmails []string) ([]*types.User, error) {
  transaction := tx
  if transaction == nil {
    transaction = ur.db
  }

  var results []*types.User
  if len(userEmails) == 0 {
    return results, nil
  }

  if err := transaction.WithContext(ctx).
    Where("email IN ?", userEmails).
    Find(&results).Error; err != nil {
    return nil, err
  }
  return results, nil
}

func (ur *userRepo) EmailExists(ctx context.Context, tx *gorm.DB, userEmail string) (bool, error) {
  transaction := tx
  if transaction == nil {
    transaction = ur.db
  }

  var count int64

  if err := transaction.WithContext(ctx).
    Model(&types.User{}).
    Where("email = ?", userEmail).
    Count(&count).Error; err != nil {
    return false, err
  }
  exists := count > 0
  return exists, nil
}










