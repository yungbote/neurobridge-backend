package services

import (
  "context"
  "fmt"
  "gorm.io/gorm"
  "github.com/google/uuid"
  "github.com/yungbote/neurobridge-backend/internal/logger"
  "github.com/yungbote/neurobridge-backend/internal/requestdata"
  "github.com/yungbote/neurobridge-backend/internal/types"
  "github.com/yungbote/neurobridge-backend/internal/repos"
)

type UserService interface {
  GetMe(ctx context.Context, tx *gorm.DB) (*types.User, error)
}

type userService struct {
  db          *gorm.DB
  log         *logger.Logger
  userRepo    repos.UserRepo
}

func NewUserService(
  db        *gorm.DB,
  log       *logger.Logger,
  userRepo  repos.UserRepo,
) UserService {
  serviceLog := log.With("service", "UserService")
  return &userService{
    db:       db,
    log:      serviceLog,
    userRepo: userRepo,
  }
}

func (us *userService) GetMe(ctx context.Context, tx *gorm.DB) (*types.User, error) {
  rd := requestdata.GetRequestData(ctx)
  if rd == nil {
    us.log.Warn("Request data not set in context")
    return nil, fmt.Errorf("Request data not set in context")
  }
  if rd.UserID == uuid.Nil {
    us.log.Warn("User id not set in request data")
    return nil, fmt.Errorf("User id not set in request data")
  }
  getUser := func(ctx context.Context, tx *gorm.DB, userID uuid.UUID) (*types.User, error) {
    foundUsers, fErr := us.userRepo.GetByIDs(ctx, tx, []uuid.UUID{userID})
    if fErr != nil {
      us.log.Warn("Error fetching user:", "error", fErr)
      return nil, fmt.Errorf("error fetching user: %w", fErr)
    }
    if len(foundUsers) == 0 {
      return nil, fmt.Errorf("user does not exist")
    }
    return foundUsers[0], nil
  }
  if tx != nil {
    return getUser(ctx, tx, rd.UserID)
  }
  var theUser *types.User
  if err := us.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
    u, err := getUser(ctx, tx, rd.UserID)
    if err != nil {
      return err
    }
    theUser = u
    return nil
  }); err != nil {
    us.log.Warn("GetMe transaction error:", "error", err)
    return nil, err
  }
  return theUser, nil
}

