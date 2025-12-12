package repos

import (
  "context"
  "github.com/google/uuid"
  "gorm.io/gorm"
  "github.com/yungbote/neurobridge-backend/internal/logger"
  "github.com/yungbote/neurobridge-backend/internal/types"
)

type UserTokenRepo interface {
  Create(ctx context.Context, tx *gorm.DB, userTokens []*types.UserToken) ([]*types.UserToken, error)
  GetByIDs(ctx context.Context, tx *gorm.DB, tokenIDs []uuid.UUID) ([]*types.UserToken, error)
  GetByUsers(ctx context.Context ,tx *gorm.DB, users []*types.User) ([]*types.UserToken, error)
  GetByUserIDs(ctx context.Context, tx *gorm.DB, userIDs []uuid.UUID) ([]*types.UserToken, error)
  GetByAccessTokens(ctx context.Context, tx *gorm.DB, accessTokens []string) ([]*types.UserToken, error)
  GetByRefreshTokens(ctx context.Context, tx *gorm.DB, refreshTokens []string) ([]*types.UserToken, error)
  SoftDeleteByTokens(ctx context.Context, tx *gorm.DB, userTokens []*types.UserToken) error
  SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, tokenIDs []uuid.UUID) error
  SoftDeleteByUserIDs(ctx context.Context, tx *gorm.DB, userIDs []uuid.UUID) error
  FullDeleteByTokens(ctx context.Context, tx *gorm.DB, userTokens []*types.UserToken) error
  FullDeleteByIDs(ctx context.Context, tx *gorm.DB, tokenIDs []uuid.UUID) error
  FullDeleteByUserIDs(ctx context.Context, tx *gorm.DB, userIDs []uuid.UUID) error
}

type userTokenRepo struct {
  db          *gorm.DB
  log         *logger.Logger
}

func NewUserTokenRepo(db *gorm.DB, baseLog *logger.Logger) UserTokenRepo {
  repoLog := baseLog.With("repo", "UserTokenRepo")
  return &userTokenRepo{db: db, log: repoLog}
}

func (utr *userTokenRepo) Create(ctx context.Context, tx *gorm.DB, userTokens []*types.UserToken) ([]*types.UserToken, error) {
  transaction := tx
  if transaction == nil {
    transaction = utr.db
  }

  if len(userTokens) == 0 {
    return []*types.UserToken{}, nil
  }

  if err := transaction.WithContext(ctx).Create(&userTokens).Error; err != nil {
    return nil, err
  }
  
  return userTokens, nil
}

func (utr *userTokenRepo) GetByIDs(ctx context.Context, tx *gorm.DB, tokenIDs []uuid.UUID) ([]*types.UserToken, error) {
  transaction := tx
  if transaction == nil {
    transaction = utr.db
  }

  var results []*types.UserToken

  if len(tokenIDs) == 0 {
    return results, nil
  }

  if err := transaction.WithContext(ctx).
    Where("id IN ?", tokenIDs).
    Find(&results).Error; err != nil {
    return nil, err
  }
  
  return results, nil
}

func (utr *userTokenRepo) GetByUsers(ctx context.Context, tx *gorm.DB, users []*types.User) ([]*types.UserToken, error) {
  transaction := tx
  if transaction == nil {
    transaction = utr.db
  }

  if len(users) == 0 {
    return []*types.UserToken{}, nil
  }

  var userIDs []uuid.UUID
  for _, u := range users {
    userIDs = append(userIDs, u.ID)
  }

  return utr.GetByUserIDs(ctx, transaction, userIDs)
}

func (utr *userTokenRepo) GetByUserIDs(ctx context.Context, tx *gorm.DB, userIDs []uuid.UUID) ([]*types.UserToken, error) {
  transaction := tx
  if transaction == nil {
    transaction = utr.db
  }

  var results []*types.UserToken
  
  if len(userIDs) == 0 {
    return results, nil
  }

  if err := transaction.WithContext(ctx).
    Where("user_id IN ?", userIDs).
    Find(&results).Error; err != nil {
    return nil, err
  }

  return results, nil
}

func (utr *userTokenRepo) GetByAccessTokens(ctx context.Context, tx *gorm.DB, accessTokens []string) ([]*types.UserToken, error) {
  transaction := tx
  if transaction == nil {
    transaction = utr.db
  }

  var results []*types.UserToken

  if len(accessTokens) == 0 {
    return results, nil
  }

  if err := transaction.WithContext(ctx).
    Where("access_token IN ?", accessTokens).
    Find(&results).Error; err != nil {
    return nil, err
  }

  return results, nil
}

func (utr *userTokenRepo) GetByRefreshTokens(ctx context.Context, tx *gorm.DB, refreshTokens []string) ([]*types.UserToken, error) {
  transaction := tx
  if transaction == nil {
    transaction = utr.db
  }

  var results []*types.UserToken
  
  if len(refreshTokens) == 0 {
    return results, nil
  }

  if err := transaction.WithContext(ctx).
    Where("refresh_token IN ?", refreshTokens).
    Find(&results).Error; err != nil {
    return nil, err
  }

  return results, nil
}

func (utr *userTokenRepo) SoftDeleteByTokens(ctx context.Context, tx *gorm.DB, userTokens []*types.UserToken) error {
  transaction := tx
  if transaction == nil {
    transaction = utr.db
  }

  if len(userTokens) == 0 {
    return nil
  }

  var tokenIDs []uuid.UUID
  for _, t := range userTokens {
    tokenIDs = append(tokenIDs, t.ID)
  }
  
  return utr.SoftDeleteByIDs(ctx, transaction, tokenIDs)
}

func (utr *userTokenRepo) SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, tokenIDs []uuid.UUID) error {
  transaction := tx
  if transaction == nil {
    transaction = utr.db
  }

  if len(tokenIDs) == 0 {
    return nil
  }

  if err := transaction.WithContext(ctx).
    Where("id IN (?)", tokenIDs).
    Delete(&types.UserToken{}).Error; err != nil {
    return err
  }

  return nil
}

func (utr *userTokenRepo) SoftDeleteByUserIDs(ctx context.Context, tx *gorm.DB, userIDs []uuid.UUID) error {
  transaction := tx
  if transaction == nil {
    transaction = utr.db
  }

  if len(userIDs) == 0 {
    return nil
  }

  if err := transaction.WithContext(ctx).
    Where("user_id IN (?)", userIDs).
    Delete(&types.UserToken{}).Error; err != nil {
    return err
  }

  return nil
}

func (utr *userTokenRepo) FullDeleteByTokens(ctx context.Context, tx *gorm.DB, userTokens []*types.UserToken) error {
  transaction := tx
  if transaction == nil {
    transaction = utr.db
  }

  if len(userTokens) == 0 {
    return nil
  }

  var tokenIDs []uuid.UUID
  for _, t := range userTokens {
    tokenIDs = append(tokenIDs, t.ID)
  }

  return utr.FullDeleteByIDs(ctx, transaction, tokenIDs)
}

func (utr *userTokenRepo) FullDeleteByIDs(ctx context.Context, tx *gorm.DB, tokenIDs []uuid.UUID) error {
  transaction := tx
  if transaction == nil {
    transaction = utr.db
  }

  if len(tokenIDs) == 0 {
    return nil
  }

  if err := transaction.WithContext(ctx).
    Unscoped().
    Where("id IN (?)", tokenIDs).
    Delete(&types.UserToken{}).Error; err != nil {
    return err
  }
  
  return nil
}

func (utr *userTokenRepo) FullDeleteByUserIDs(ctx context.Context, tx *gorm.DB, userIDs []uuid.UUID) error {
  transaction := tx
  if transaction == nil {
    transaction = utr.db
  }

  if len(userIDs) == 0 {
    return nil
  }

  if err := transaction.WithContext(ctx).
    Unscoped().
    Where("user_id IN (?)", userIDs).
    Delete(&types.UserToken{}).Error; err != nil {
    return err
  }

  return nil
}










