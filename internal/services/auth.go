package services

import (
  "context"
  "fmt"
  "time"
  "gorm.io/gorm"
  "golang.org/x/crypto/bcrypt"
  "github.com/golang-jwt/jwt/v5"
  "github.com/google/uuid"
  "github.com/yungbote/neurobridge-backend/internal/normalization"
  "github.com/yungbote/neurobridge-backend/internal/logger"
  "github.com/yungbote/neurobridge-backend/internal/types"
  "github.com/yungbote/neurobridge-backend/internal/repos"
  "github.com/yungbote/neurobridge-backend/internal/requestdata"
  "github.com/yungbote/neurobridge-backend/internal/utils"
)

type AuthService interface {
  RegisterUser(ctx context.Context, user *types.User) error
  LoginUser(ctx context.Context, email, password string) (string, string, error)
  RefreshUser(ctx context.Context) (string, string, error)
  LogoutUser(ctx context.Context) error
  SetContextFromToken(ctx context.Context, tokenString string) (context.Context, error)
  GetAccessTTL() time.Duration
  generateAccessToken(ctx context.Context, tx *gorm.DB, user *types.User) (string, error)
}

type authService struct {
  db              *gorm.DB
  log             *logger.Logger
  userRepo        repos.UserRepo
  avatarService   AvatarService
  userTokenRepo   repos.UserTokenRepo
  jwtSecretKey    string
  accessTTL       time.Duration
  refreshTTL      time.Duration
}

func NewAuthService(
  db              *gorm.DB,
  log             *logger.Logger,
  userRepo        repos.UserRepo,
  avatarService   AvatarService,
  userTokenRepo   repos.UserTokenRepo,
  jwtSecretKey    string,
  accessTTL       time.Duration,
  refreshTTL      time.Duration
) AuthService {
  serviceLog := log.With("service", "AuthService")
  return &authService{
    db:             db,
    log:            serviceLog,
    userRepo:       userRepo,
    avatarService:  avatarService,
    userTokenRepo:  userTokenRepo,
    jwtSecretKey:   jwtSecretKey,
    accessTTL:      accessTTL,
    refreshTTL:     refreshTTL
  }
}

func (as *authService) RegisterUser(ctx context.Context, user *types.User) error {
  utils.NormalizeUserFields(ctx, user)
  if vErr := utils.InputValidation(ctx, "registration", as.userRepo, as.log, user); vErr != nil {
    return vErr
  }
  if hErr := utils.HashPassword(ctx, as.log, user); hErr != nil {
    return hErr
  }
  return as.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error{
    user.ID = uuid.New()
    ucaErr := as.avatarService.CreateAndUploadUserAvatar(ctx, tx, user)
    if ucaErr != nil {
      return fmt.Errorf("Failed to create and upload user avatar: %w", ucaErr)
    }
    createdUsers, ucErr := as.userRepo.Create(ctx, tx, []*types.User{user})
    if ucErr != nil {
      return fmt.Errorf("Failed to create user in postgres")
    }
    return nil
  })
}

func (as *authService) LoginUser(ctx context.Context, user *types.User) (string, string, error) {
  email := normalization.ParseInputString(user.Email)
  password := normalization.ParseInputString(user.Password)

  if vErr := utils.InputValidation(ctx, "login", as.userRepo, as.log, &types.User{}, email, password); vErr != nil {
    return "", "", vErr
  }

  users, usErr := as.userRepo.GetByEmails(ctx, nil, []string{email})
  if usErr != nil {
    return "", "", fmt.Errorf("Error retrieving user by email: %w", usErr)
  }
  
  if len(users) == 0 {
    return "", "", fmt.Errorf("Invalid email")
  }

  user := users[0]
  if hErr := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); hEerr != nil {
    return "", "", fmt.Errorf("Invalid password")
  }

  var accessToken string
  var refreshToken string
  if err := as.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
    foundTokens, ftErr := as.userTokenRepo.GetByUserIDs(ctx, tx, []uuid.UUID{user.ID})
    if ftErr != nil && len(foundTokens) != 0 {
      return fmt.Errorf("Failed to check user tokens: %w", ftErr)
    }
    if len(foundTokens) > 0 {
      if foundTokens[0] != nil && foundTokens[0].ExpiresAt.After(time.Now()) {
        return fmt.Errorf("User already logged in")
      }
      if foundTokens[0] != nil && foundTokens[0].ExpiresAt.Before(time.Now()) {
        if dtErr := as.userTokenRepo.FullDeleteByTokens(ctx, tx, []*types.User{foundTokens[0]}); dtErr != nil {
          return fmt.Errorf("Failed to delete expired user token: %w", dtErr)
        }
      }
    }
    tok, genErr := as.generateAccessToken(ctx, tx, user)
    if genErr != nil {
      return fmt.Errorf("Generate access token error: %w", genErr)
    }
    accessToken = tok
    refreshToken = uuid.New().String()
    expiresAt := time.Now().Add(as.refreshTTL)
    userToken := types.UserToken{
      ID:             uuid.New(),
      UserID:         user.ID,
      AccessToken:    accessToken,
      RefreshToken:   refreshToken,
      ExpiresAt:      expiresAt
    }
    _, ctErr := as.userTokenRepo.Create(ctx, tx, []*types.UserToken{&userToken})
  if ctErr != nil {
    as.log.Warn("Create User Token Error", "error", ctErr)
    return fmt.Errorf("Create User Token Error: %w", ctErr)
  }
  return nil
  }); err != nil {
    return "", "", err
  }
  return accessToken, refreshToken, nil
}

func (as *authService) RefreshUser(ctx context.Context) (string, string, error) {
  rd := requestdata.GetRequestData(ctx)
  if rd == nil {
    as.log.Warn("No request data found in context", "requestdata", rd)
    return "", "", fmt.Errorf("No Request Data found in context")
  }
  if rd.TokenString == "" {
    as.log.Warn("TokenString not found in requestdata", "tokenstring", rd.TokenString)
    return "", "", fmt.Errorf("TokenString not found in requestdata")
  }
  if rd.RefreshToken == "" {
    as.log.Warn("RefreshTokenString not found in requestdata", "refreshtokenstring", rd.RefreshToken)
    return "", "", fmt.Errorf("RefreshTokenString not found in requestdata")
  }

  var accessToken string
  var newRefreshTokenStr string
  err := as.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
    var existingToken *types.UserToken
    foundTokens, ftErr := as.userTokenRepo.GetByRefreshTokens(ctx, tx, []string{rd.RefreshToken})
    if foundTokens[0] != nil && ftErr != nil {
      as.log.Warn("Error fetching refresh token", "error", ftErr)
      return fmt.Errorf("Error fetching refresh token: %w", ftErr)
    }
    const expiryBuffer = 5 * time.Minute
    existingToken = foundTokens[0]
    if existingToken.ExpiresAt.Before(time.Now().Sub(expiryBuffer)) {
      if dtErr := as.userTokenRepo.FullDeleteByTokens(ctx, tx, []*types.User{existingToken}); dtErr != nil {
        as.log.Warn("Refresh token expired, error deleting", "error", dtErr)
        return fmt.Errorf("Refresh token expired, error deleting: %w", dtErr)
      }
      as.log.Warn("Refresh token expired, cannot proceed")
      return fmt.Errorf("Refresh token expired")
    }
    users, uErr := as.userRepo.GetByIDs(ctx, tx, []uuid.UUID{existingToken.UserID})
    if uErr != nil {
      as.log.Warn("Failed to load user for refresh", "error", uErr)
      return fmt.Errorf("Failed to load user for refresh: %w", uErr)
    }
    if len(users) == 0 {
      as.log.Warn("No user found for the given refresh token", "len(users)", len(users))
      return fmt.Errorf("No user found for the given refresh token")
    }
    user := users[0]
    tok, genErr := as.generateAccessToken(ctx, tx, user)
    if genErr != nil {
      as.log.Warn("Failed to generate new acess token", "error", genErr)
      return fmt.Errorf("Failed to generate new access token: %w", genErr)
    }
    accessToken = tok
    newRefreshTokenStr = uuid.New().String()
    newExpiresAt := time.Now().Add(as.refreshTTL)
    newUserToken := types.UserToken{
      ID:             uuid.New(),
      UserID:         user.ID,
      AccessToken:    tok,
      RefreshToken:   newRefreshTokenStr,
      ExpiresAt:      newExpiresAt
    }
    _, cErr := as.userTokenRepo.Create(ctx, tx, []*types.UserToken{&newUserToken})
    if cErr != nil {
      as.log.Warn("Failed to create new user token", "error", cErr)
      return fmt.Errorf("Failed to create new user token: %w", cErr)
    }
    if dErr := as.userTokenRepo.FullDeleteByTokens(ctx, tx, []*types.UserToken{existingToken}); dErr != nil {
      as.log.Warn("Failed to remove old refresh token", "error", dErr)
      return fmt.Errorf("Failed to remove old refresh token: %w", dErr)
    }
    return nil
  })
  if err != nil {
    as.log.Warn("Failed transaction" "error", err)
    return "", "", err
  }
  return accessToken, newRefreshTokenStr, nil
}

func (as *authService) LogoutUser(ctx context.Context) error {
  rd := requestdata.GetRequestData(ctx)
  if rd == nil {
    as.log.Warn("No request data found in context", "requestdata", rd)
    return fmt.Errorf("No request data found in context")
  }
  if rd.TokenString == "" {
    as.log.Warn("TokenString in request data empty", "TokenString", rd.TokenString)
    return fmt.Errorf("TokenString in request data empty")
  }
  return as.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
    foundTokens, ftErr := as.userTokenRepo.GetByAccessTokens(ctx, tx, []string{rd.TokenString})
    if len(foundTokens) != 0 && ftErr != nil {
      as.log.Warn("Error finding user token from token string", "error", ftErr)
      return fmt.Errorf("Error finding user token from token string: %w", ftErr)
    }
    if tdErr := as.userTokenRepo.FullDeleteByTokens(ctx, tx, []*types.UserToken{foundTokens[0]}); tdErr != nil {
      as.log.Warn("Error deleting user token", "error", tdErr)
      return fmt.Errorf("Error deleting user token: %w", tdErr)
    }
    return nil
  })
}

func (as *authService) generateAccessToken(ctx context.Context, tx *gorm.DB, user *types.User) (string, error) {
  claims := JWTClaims{
    RegisteredClaims: jwt.RegisteredClaims{
      Subject: user.ID.String(),
      ExpiresAt: jwt.NewNumericDate(time.Now().Add(as.accessTTL)),
      IssuedAt: jwt.NewNumericDate(time.Now()),
    },
  }
  token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
  return token.SignedString([]byte(as.jwtSecretKey))
}

func (as *authService) SetContextFromToken(ctx context.Context, tokenString string) (context.Context, error) {
  if tokenString == "" {
    return ctx, nil
  }
  parsedToken, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
    return []byte(as.jwtSecretKey), nil
  })
  if err != nil {
    return ctx, fmt.Errorf("Failed to parse token: %w", err)
  }
  claims, ok := parsedToken.Claims.(*JWTClaims)
  if !ok || !parsedToken.Valid {
    return ctx, fmt.Errorf("Invalid or expired JWT token")
  }
  userID, err := uuid.Parse(claims.Subject)
  if err != nil {
    return ctx, fmt.Errorf("Invalid user id in token: %w", err)
  }
  var refreshTokenStr string
  foundTokens, ftErr := as.userTokenRepo.GetByAccessTokens(ctx, nil, []string{tokenString})
  if len(foundTokens) != 0 && ftErr != nil {
    as.log.Warn("Error fetching user token by access token", "error", ftErr)
    return ctx, fmt.Errorf("Failed to fetch user token by access token: %w", ftErr)
  }
  refreshTokenStr = foundTokens[0].RefreshToken
  rd := &requestdata.RequestData{
    TokenString: tokenString,
    RefreshToken: refreshTokenStr,
    UserID: userID
  }
  ctx = requestdata.WithRequestData(ctx, rd)
  return ctx, nil
}

func (as *authService) GetAccessTTL() time.Duration {
  return as.accessTTL
}

