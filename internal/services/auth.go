package services

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/logger"
	"github.com/yungbote/neurobridge-backend/internal/normalization"
	"github.com/yungbote/neurobridge-backend/internal/repos"
	"github.com/yungbote/neurobridge-backend/internal/requestdata"
	"github.com/yungbote/neurobridge-backend/internal/types"
	"github.com/yungbote/neurobridge-backend/internal/utils"
)

type JWTClaims struct {
	jwt.RegisteredClaims
}

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
	db            *gorm.DB
	log           *logger.Logger
	userRepo      repos.UserRepo
	avatarService AvatarService

	userTokenRepo repos.UserTokenRepo

	jwtSecretKey string
	accessTTL    time.Duration
	refreshTTL   time.Duration
}

func NewAuthService(
	db *gorm.DB,
	log *logger.Logger,
	userRepo repos.UserRepo,
	avatarService AvatarService,
	userTokenRepo repos.UserTokenRepo,
	jwtSecretKey string,
	accessTTL time.Duration,
	refreshTTL time.Duration,
) AuthService {
	serviceLog := log.With("service", "AuthService")
	return &authService{
		db:            db,
		log:           serviceLog,
		userRepo:      userRepo,
		avatarService: avatarService,
		userTokenRepo: userTokenRepo,
		jwtSecretKey:  jwtSecretKey,
		accessTTL:     accessTTL,
		refreshTTL:    refreshTTL,
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
	return as.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		user.ID = uuid.New()
		if ucaErr := as.avatarService.CreateAndUploadUserAvatar(ctx, tx, user); ucaErr != nil {
			return fmt.Errorf("failed to create and upload user avatar: %w", ucaErr)
		}
		if _, ucErr := as.userRepo.Create(ctx, tx, []*types.User{user}); ucErr != nil {
			return fmt.Errorf("failed to create user in postgres: %w", ucErr)
		}
		return nil
	})
}

func (as *authService) LoginUser(ctx context.Context, email, password string) (string, string, error) {
	email = normalization.ParseInputString(email)
	password = normalization.ParseInputString(password)

	if vErr := utils.InputValidation(ctx, "login", as.userRepo, as.log, &types.User{}, email, password); vErr != nil {
		return "", "", vErr
	}

	users, usErr := as.userRepo.GetByEmails(ctx, nil, []string{email})
	if usErr != nil {
		return "", "", fmt.Errorf("error retrieving user by email: %w", usErr)
	}
	if len(users) == 0 {
		return "", "", fmt.Errorf("invalid email")
	}

	user := users[0]
	if hErr := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); hErr != nil {
		return "", "", fmt.Errorf("invalid password")
	}

	var accessToken string
	var refreshToken string

	// IMPORTANT: allow multiple tokens per user; just clean up expired ones.
	if err := as.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		foundTokens, ftErr := as.userTokenRepo.GetByUserIDs(ctx, tx, []uuid.UUID{user.ID})
		if ftErr != nil {
			as.log.Warn("Failed to check user tokens", "error", ftErr)
			return fmt.Errorf("failed to check user tokens: %w", ftErr)
		}

		// Clean up expired tokens, but do NOT block login if existing tokens are valid.
		now := time.Now()
		var expired []*types.UserToken
		for _, t := range foundTokens {
			if t != nil && t.ExpiresAt.Before(now) {
				expired = append(expired, t)
			}
		}
		if len(expired) > 0 {
			if dtErr := as.userTokenRepo.FullDeleteByTokens(ctx, tx, expired); dtErr != nil {
				as.log.Warn("Failed to delete expired user tokens", "error", dtErr)
				return fmt.Errorf("failed to delete expired user tokens: %w", dtErr)
			}
		}

		// Generate new access token + refresh token
		tok, genErr := as.generateAccessToken(ctx, tx, user)
		if genErr != nil {
			return fmt.Errorf("generate access token error: %w", genErr)
		}
		accessToken = tok
		refreshToken = uuid.New().String()
		expiresAt := time.Now().Add(as.refreshTTL)

		userToken := types.UserToken{
			ID:           uuid.New(),
			UserID:       user.ID,
			AccessToken:  accessToken,
			RefreshToken: refreshToken,
			ExpiresAt:    expiresAt,
		}
		if _, ctErr := as.userTokenRepo.Create(ctx, tx, []*types.UserToken{&userToken}); ctErr != nil {
			as.log.Warn("Create User Token Error", "error", ctErr)
			return fmt.Errorf("create user token error: %w", ctErr)
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
		as.log.Warn("No request data found in context")
		return "", "", fmt.Errorf("no request data found in context")
	}
	if strings.TrimSpace(rd.TokenString) == "" {
		as.log.Warn("TokenString in request data is empty")
		return "", "", fmt.Errorf("token string in request data is empty")
	}
	if strings.TrimSpace(rd.RefreshToken) == "" {
		as.log.Warn("RefreshToken in request data is empty")
		return "", "", fmt.Errorf("refresh token in request data is empty")
	}

	var accessToken string
	var newRefreshTokenStr string

	err := as.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		foundTokens, ftErr := as.userTokenRepo.GetByRefreshTokens(ctx, tx, []string{rd.RefreshToken})
		if ftErr != nil {
			as.log.Warn("Error fetching refresh token", "error", ftErr)
			return fmt.Errorf("error fetching refresh token: %w", ftErr)
		}
		if len(foundTokens) == 0 || foundTokens[0] == nil {
			as.log.Warn("Refresh token not found")
			return fmt.Errorf("refresh token not found")
		}
		existingToken := foundTokens[0]

		// Small buffer: consider really expired if it's been expired for more than 5 minutes
		buffer := 5 * time.Minute
		if existingToken.ExpiresAt.Add(buffer).Before(time.Now()) {
			if dtErr := as.userTokenRepo.FullDeleteByTokens(ctx, tx, []*types.UserToken{existingToken}); dtErr != nil {
				as.log.Warn("Refresh token expired, error deleting", "error", dtErr)
				return fmt.Errorf("refresh token expired, error deleting: %w", dtErr)
			}
			as.log.Warn("Refresh token expired")
			return fmt.Errorf("refresh token expired")
		}

		users, uErr := as.userRepo.GetByIDs(ctx, tx, []uuid.UUID{existingToken.UserID})
		if uErr != nil {
			as.log.Warn("Failed to load user for refresh", "error", uErr)
			return fmt.Errorf("failed to load user for refresh: %w", uErr)
		}
		if len(users) == 0 {
			as.log.Warn("No user found for the given refresh token")
			return fmt.Errorf("no user found for the given refresh token")
		}
		user := users[0]

		tok, genErr := as.generateAccessToken(ctx, tx, user)
		if genErr != nil {
			as.log.Warn("Failed to generate new access token", "error", genErr)
			return fmt.Errorf("failed to generate new access token: %w", genErr)
		}
		accessToken = tok
		newRefreshTokenStr = uuid.New().String()
		newExpiresAt := time.Now().Add(as.refreshTTL)

		newUserToken := types.UserToken{
			ID:           uuid.New(),
			UserID:       user.ID,
			AccessToken:  tok,
			RefreshToken: newRefreshTokenStr,
			ExpiresAt:    newExpiresAt,
		}
		if _, cErr := as.userTokenRepo.Create(ctx, tx, []*types.UserToken{&newUserToken}); cErr != nil {
			as.log.Warn("Failed to create new user token", "error", cErr)
			return fmt.Errorf("failed to create new user token: %w", cErr)
		}
		if dErr := as.userTokenRepo.FullDeleteByTokens(ctx, tx, []*types.UserToken{existingToken}); dErr != nil {
			as.log.Warn("Failed to remove old refresh token", "error", dErr)
			return fmt.Errorf("failed to remove old refresh token: %w", dErr)
		}
		return nil
	})
	if err != nil {
		as.log.Warn("Refresh transaction failed", "error", err)
		return "", "", err
	}
	return accessToken, newRefreshTokenStr, nil
}

func (as *authService) LogoutUser(ctx context.Context) error {
	rd := requestdata.GetRequestData(ctx)
	if rd == nil {
		as.log.Warn("No request data found in context")
		return fmt.Errorf("no request data found in context")
	}
	if strings.TrimSpace(rd.TokenString) == "" {
		as.log.Warn("TokenString in request data empty")
		return fmt.Errorf("token string in request data empty")
	}
	return as.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		foundTokens, ftErr := as.userTokenRepo.GetByAccessTokens(ctx, tx, []string{rd.TokenString})
		if ftErr != nil {
			as.log.Warn("Error finding user token from token string", "error", ftErr)
			return fmt.Errorf("error finding user token from token string: %w", ftErr)
		}
		if len(foundTokens) == 0 || foundTokens[0] == nil {
			return nil
		}
		if tdErr := as.userTokenRepo.FullDeleteByTokens(ctx, tx, []*types.UserToken{foundTokens[0]}); tdErr != nil {
			as.log.Warn("Error deleting user token", "error", tdErr)
			return fmt.Errorf("error deleting user token: %w", tdErr)
		}
		return nil
	})
}

func (as *authService) generateAccessToken(ctx context.Context, tx *gorm.DB, user *types.User) (string, error) {
	claims := JWTClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   user.ID.String(),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(as.accessTTL)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(as.jwtSecretKey))
}

func (as *authService) SetContextFromToken(ctx context.Context, tokenString string) (context.Context, error) {
	if strings.TrimSpace(tokenString) == "" {
		return ctx, nil
	}
	parsedToken, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
		return []byte(as.jwtSecretKey), nil
	})
	if err != nil {
		return ctx, fmt.Errorf("failed to parse token: %w", err)
	}
	claims, ok := parsedToken.Claims.(*JWTClaims)
	if !ok || !parsedToken.Valid {
		return ctx, fmt.Errorf("invalid or expired JWT token")
	}
	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return ctx, fmt.Errorf("invalid user id in token: %w", err)
	}

	foundTokens, ftErr := as.userTokenRepo.GetByAccessTokens(ctx, nil, []string{tokenString})
	if ftErr != nil {
		as.log.Warn("Error fetching user token by access token", "error", ftErr)
		return ctx, fmt.Errorf("failed to fetch user token by access token: %w", ftErr)
	}
	if len(foundTokens) == 0 || foundTokens[0] == nil {
		return ctx, fmt.Errorf("user token not found for access token")
	}
	existingToken := foundTokens[0]

	rd := &requestdata.RequestData{
		TokenString:  tokenString,
		RefreshToken: existingToken.RefreshToken,
		UserID:       userID,
		SessionID:    existingToken.ID, // this token = this session
	}
	ctx = requestdata.WithRequestData(ctx, rd)
	return ctx, nil
}

func (as *authService) GetAccessTTL() time.Duration {
	return as.accessTTL
}

