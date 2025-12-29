package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/ctxutil"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"github.com/yungbote/neurobridge-backend/internal/pkg/normalize"
	"github.com/yungbote/neurobridge-backend/internal/utils"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"strings"
	"time"
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
	generateAccessToken(dbc dbctx.Context, user *types.User) (string, error)
	CreateOAuthNonce(ctx context.Context, provider string) (nonceID uuid.UUID, nonce string, expiresInSeconds int, err error)
	OAuthLoginGoogle(ctx context.Context, idToken string, nonceID uuid.UUID, firstName, lastName string) (string, string, error)
	OAuthLoginApple(ctx context.Context, idToken string, nonceID uuid.UUID, firstName, lastName string) (string, string, error)
	issueSession(dbc dbctx.Context, user *types.User) (string, string, error)
	findOrCreateUserForExternalIdentity(dbc dbctx.Context, provider string, ext *ExternalIdentity, fallbackFirst string, fallbackLast string) (*types.User, error)
	oauthLogin(ctx context.Context, provider string, idToken string, nonceID uuid.UUID, firstName, lastName string) (string, string, error)
}

type authService struct {
	db               *gorm.DB
	log              *logger.Logger
	userRepo         repos.UserRepo
	avatarService    AvatarService
	userTokenRepo    repos.UserTokenRepo
	userIdentityRepo repos.UserIdentityRepo
	oauthNonceRepo   repos.OAuthNonceRepo
	oidcVerifier     OIDCVerifier
	jwtSecretKey     string
	accessTTL        time.Duration
	refreshTTL       time.Duration
	oauthNonceTTL    time.Duration
}

func NewAuthService(
	db *gorm.DB,
	log *logger.Logger,
	userRepo repos.UserRepo,
	avatarService AvatarService,
	userTokenRepo repos.UserTokenRepo,
	userIdentityRepo repos.UserIdentityRepo,
	oauthNonceRepo repos.OAuthNonceRepo,
	oidcVerifier OIDCVerifier,
	jwtSecretKey string,
	accessTTL time.Duration,
	refreshTTL time.Duration,
	oauthNonceTTL time.Duration,
) AuthService {
	serviceLog := log.With("service", "AuthService")
	return &authService{
		db:               db,
		log:              serviceLog,
		userRepo:         userRepo,
		avatarService:    avatarService,
		userTokenRepo:    userTokenRepo,
		userIdentityRepo: userIdentityRepo,
		oauthNonceRepo:   oauthNonceRepo,
		oidcVerifier:     oidcVerifier,
		jwtSecretKey:     jwtSecretKey,
		accessTTL:        accessTTL,
		refreshTTL:       refreshTTL,
		oauthNonceTTL:    oauthNonceTTL,
	}
}

func (as *authService) RegisterUser(ctx context.Context, user *types.User) error {
	utils.NormalizeUserFields(ctx, user)
	if vErr := utils.InputValidation(ctx, "registration", as.userRepo, as.log, user, "", ""); vErr != nil {
		return vErr
	}
	if hErr := utils.HashPassword(ctx, as.log, user); hErr != nil {
		return hErr
	}
	return as.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		dbc := dbctx.Context{Ctx: ctx, Tx: tx}
		user.ID = uuid.New()
		if ucaErr := as.avatarService.CreateAndUploadUserAvatar(dbc, user); ucaErr != nil {
			return fmt.Errorf("failed to create and upload user avatar: %w", ucaErr)
		}
		if _, ucErr := as.userRepo.Create(dbc, []*types.User{user}); ucErr != nil {
			return fmt.Errorf("failed to create user in postgres: %w", ucErr)
		}
		return nil
	})
}

func (as *authService) LoginUser(ctx context.Context, email, password string) (string, string, error) {
	email = normalize.ParseInputString(email)
	password = normalize.ParseInputString(password)

	if vErr := utils.InputValidation(ctx, "login", as.userRepo, as.log, &types.User{}, email, password); vErr != nil {
		return "", "", vErr
	}

	users, usErr := as.userRepo.GetByEmails(dbctx.Context{Ctx: ctx}, []string{email})
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
		dbc := dbctx.Context{Ctx: ctx, Tx: tx}
		foundTokens, ftErr := as.userTokenRepo.GetByUserIDs(dbc, []uuid.UUID{user.ID})
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
			if dtErr := as.userTokenRepo.FullDeleteByTokens(dbc, expired); dtErr != nil {
				as.log.Warn("Failed to delete expired user tokens", "error", dtErr)
				return fmt.Errorf("failed to delete expired user tokens: %w", dtErr)
			}
		}

		// Generate new access token + refresh token
		tok, genErr := as.generateAccessToken(dbc, user)
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
		if _, ctErr := as.userTokenRepo.Create(dbc, []*types.UserToken{&userToken}); ctErr != nil {
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
	rd := ctxutil.GetRequestData(ctx)
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
		dbc := dbctx.Context{Ctx: ctx, Tx: tx}
		foundTokens, ftErr := as.userTokenRepo.GetByRefreshTokens(dbc, []string{rd.RefreshToken})
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
			if dtErr := as.userTokenRepo.FullDeleteByTokens(dbc, []*types.UserToken{existingToken}); dtErr != nil {
				as.log.Warn("Refresh token expired, error deleting", "error", dtErr)
				return fmt.Errorf("refresh token expired, error deleting: %w", dtErr)
			}
			as.log.Warn("Refresh token expired")
			return fmt.Errorf("refresh token expired")
		}

		users, uErr := as.userRepo.GetByIDs(dbc, []uuid.UUID{existingToken.UserID})
		if uErr != nil {
			as.log.Warn("Failed to load user for refresh", "error", uErr)
			return fmt.Errorf("failed to load user for refresh: %w", uErr)
		}
		if len(users) == 0 {
			as.log.Warn("No user found for the given refresh token")
			return fmt.Errorf("no user found for the given refresh token")
		}
		user := users[0]

		tok, genErr := as.generateAccessToken(dbc, user)
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
		if _, cErr := as.userTokenRepo.Create(dbc, []*types.UserToken{&newUserToken}); cErr != nil {
			as.log.Warn("Failed to create new user token", "error", cErr)
			return fmt.Errorf("failed to create new user token: %w", cErr)
		}
		if dErr := as.userTokenRepo.FullDeleteByTokens(dbc, []*types.UserToken{existingToken}); dErr != nil {
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
	rd := ctxutil.GetRequestData(ctx)
	if rd == nil {
		as.log.Warn("No request data found in context")
		return fmt.Errorf("no request data found in context")
	}
	if strings.TrimSpace(rd.TokenString) == "" {
		as.log.Warn("TokenString in request data empty")
		return fmt.Errorf("token string in request data empty")
	}
	return as.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		dbc := dbctx.Context{Ctx: ctx, Tx: tx}
		foundTokens, ftErr := as.userTokenRepo.GetByAccessTokens(dbc, []string{rd.TokenString})
		if ftErr != nil {
			as.log.Warn("Error finding user token from token string", "error", ftErr)
			return fmt.Errorf("error finding user token from token string: %w", ftErr)
		}
		if len(foundTokens) == 0 || foundTokens[0] == nil {
			return nil
		}
		if tdErr := as.userTokenRepo.FullDeleteByTokens(dbc, []*types.UserToken{foundTokens[0]}); tdErr != nil {
			as.log.Warn("Error deleting user token", "error", tdErr)
			return fmt.Errorf("error deleting user token: %w", tdErr)
		}
		return nil
	})
}

func (as *authService) CreateOAuthNonce(ctx context.Context, provider string) (uuid.UUID, string, int, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider != "google" && provider != "apple" {
		return uuid.Nil, "", 0, fmt.Errorf("unsupported provider")
	}
	nonce := randomNonce(32) // raw nonce returned to client
	nonceHash := hashNonceBase64URL(nonce)
	expiresAt := time.Now().Add(as.oauthNonceTTL)
	n := &types.OAuthNonce{
		ID:        uuid.New(),
		Provider:  provider,
		NonceHash: nonceHash,
		ExpiresAt: expiresAt,
	}
	if err := as.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		_, err := as.oauthNonceRepo.Create(dbctx.Context{Ctx: ctx, Tx: tx}, []*types.OAuthNonce{n})
		return err
	}); err != nil {
		return uuid.Nil, "", 0, fmt.Errorf("failed to create oauth nonce: %w", err)
	}
	return n.ID, nonce, int(as.oauthNonceTTL.Seconds()), nil
}

func (as *authService) OAuthLoginGoogle(ctx context.Context, idToken string, nonceID uuid.UUID, firstName, lastName string) (string, string, error) {
	return as.oauthLogin(ctx, "google", idToken, nonceID, firstName, lastName)
}

func (as *authService) OAuthLoginApple(ctx context.Context, idToken string, nonceID uuid.UUID, firstName, lastName string) (string, string, error) {
	return as.oauthLogin(ctx, "apple", idToken, nonceID, firstName, lastName)
}

func (as *authService) oauthLogin(ctx context.Context, provider string, idToken string, nonceID uuid.UUID, firstName, lastName string) (string, string, error) {
	if nonceID == uuid.Nil {
		return "", "", fmt.Errorf("nonce_id is required")
	}
	var accessToken, refreshToken string
	err := as.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		dbc := dbctx.Context{Ctx: ctx, Tx: tx}
		// 1) Load nonce record
		nonces, err := as.oauthNonceRepo.GetByIDs(dbc, []uuid.UUID{nonceID})
		if err != nil {
			return fmt.Errorf("failed to load oauth nonce: %w", err)
		}
		if len(nonces) == 0 || nonces[0] == nil {
			return fmt.Errorf("oauth nonce not found")
		}
		n := nonces[0]
		if n.Provider != provider {
			return fmt.Errorf("nonce provider mismatch")
		}
		if n.UsedAt != nil {
			return fmt.Errorf("oauth nonce already used")
		}
		if time.Now().After(n.ExpiresAt) {
			return fmt.Errorf("oauth nonce expired")
		}
		// 2) Verify ID token INCLUDING nonce
		var ext *ExternalIdentity
		switch provider {
		case "google":
			ext, err = as.oidcVerifier.VerifyGoogleIDToken(ctx, idToken, n.NonceHash)
		case "apple":
			ext, err = as.oidcVerifier.VerifyAppleIDToken(ctx, idToken, n.NonceHash)
		default:
			return fmt.Errorf("unsupported provider")
		}
		if err != nil {
			return fmt.Errorf("id_token verification failed: %w", err)
		}
		// 3) Consume nonce (single-use) AFTER verification but BEFORE issuing session
		if err := as.oauthNonceRepo.MarkUsed(dbc, n.ID); err != nil {
			return fmt.Errorf("failed to consume nonce: %w", err)
		}
		// 4) Find or create user (this is signup+login in one)
		user, err := as.findOrCreateUserForExternalIdentity(dbc, provider, ext, firstName, lastName)
		if err != nil {
			return err
		}
		// 5) Issue your normal session (JWT + refresh)
		a, r, err := as.issueSession(dbc, user)
		if err != nil {
			return err
		}
		accessToken, refreshToken = a, r
		return nil
	})
	if err != nil {
		return "", "", err
	}
	return accessToken, refreshToken, nil
}

func (as *authService) findOrCreateUserForExternalIdentity(
	dbc dbctx.Context,
	provider string,
	ext *ExternalIdentity,
	fallbackFirst string,
	fallbackLast string,
) (*types.User, error) {
	if ext == nil || strings.TrimSpace(ext.Sub) == "" {
		return nil, fmt.Errorf("invalid external identity")
	}
	// 1) lookup by (provider, sub)
	ids, err := as.userIdentityRepo.GetByProviderSubs(dbc, provider, []string{ext.Sub})
	if err != nil {
		return nil, fmt.Errorf("failed to lookup identity: %w", err)
	}
	if len(ids) > 0 && ids[0] != nil {
		users, uErr := as.userRepo.GetByIDs(dbc, []uuid.UUID{ids[0].UserID})
		if uErr != nil {
			return nil, fmt.Errorf("failed to load user for identity: %w", uErr)
		}
		if len(users) == 0 {
			return nil, fmt.Errorf("user not found for identity")
		}
		return users[0], nil
	}
	// 2) OPTIONAL: link to existing user by verified email to avoid dup accounts
	var existingUser *types.User
	if ext.EmailVerified && strings.TrimSpace(ext.Email) != "" {
		found, err := as.userRepo.GetByEmails(dbc, []string{ext.Email})
		if err != nil {
			return nil, fmt.Errorf("failed to lookup user by email: %w", err)
		}
		if len(found) > 0 {
			existingUser = found[0]
		}
	}
	// 3) Create user if needed
	if existingUser == nil {
		if strings.TrimSpace(ext.Email) == "" {
			// You require user.email NOT NULL; Apple will provide email on first authorization.
			return nil, fmt.Errorf("email_required_for_first_time_oauth_signup")
		}
		first := strings.TrimSpace(ext.FirstName)
		last := strings.TrimSpace(ext.LastName)
		if first == "" {
			first = strings.TrimSpace(fallbackFirst)
		}
		if last == "" {
			last = strings.TrimSpace(fallbackLast)
		}
		if first == "" {
			first = "New"
		}
		if last == "" {
			last = "User"
		}
		u := &types.User{
			ID:        uuid.New(),
			Email:     ext.Email,
			FirstName: first,
			LastName:  last,
		}
		// Password is NOT NULL; set random hashed password so password-login isn't usable unless you add "set password".
		raw := randomNonce(48)
		hashed, err := bcrypt.GenerateFromPassword([]byte(raw), bcrypt.DefaultCost)
		if err != nil {
			return nil, fmt.Errorf("failed to hash generated password: %w", err)
		}
		u.Password = string(hashed)
		if ucaErr := as.avatarService.CreateAndUploadUserAvatar(dbc, u); ucaErr != nil {
			return nil, fmt.Errorf("failed to create and upload user avatar: %w", ucaErr)
		}
		if _, err := as.userRepo.Create(dbc, []*types.User{u}); err != nil {
			return nil, fmt.Errorf("failed to create user: %w", err)
		}
		existingUser = u
	}
	// 4) Create identity mapping
	ui := &types.UserIdentity{
		ID:            uuid.New(),
		UserID:        existingUser.ID,
		Provider:      provider,
		ProviderSub:   ext.Sub,
		Email:         ext.Email,
		EmailVerified: ext.EmailVerified,
	}
	if _, err := as.userIdentityRepo.Create(dbc, []*types.UserIdentity{ui}); err != nil {
		return nil, fmt.Errorf("failed to create user identity: %w", err)
	}
	return existingUser, nil
}

func (as *authService) issueSession(dbc dbctx.Context, user *types.User) (string, string, error) {
	// clean expired tokens
	foundTokens, ftErr := as.userTokenRepo.GetByUserIDs(dbc, []uuid.UUID{user.ID})
	if ftErr != nil {
		as.log.Warn("Failed to check user tokens", "error", ftErr)
		return "", "", fmt.Errorf("failed to check user tokens: %w", ftErr)
	}
	now := time.Now()
	var expired []*types.UserToken
	for _, t := range foundTokens {
		if t != nil && t.ExpiresAt.Before(now) {
			expired = append(expired, t)
		}
	}
	if len(expired) > 0 {
		if dtErr := as.userTokenRepo.FullDeleteByTokens(dbc, expired); dtErr != nil {
			as.log.Warn("Failed to delete expired user tokens", "error", dtErr)
			return "", "", fmt.Errorf("failed to delete expired user tokens: %w", dtErr)
		}
	}
	accessToken, err := as.generateAccessToken(dbc, user)
	if err != nil {
		return "", "", fmt.Errorf("generate access token error: %w", err)
	}
	refreshToken := uuid.New().String()
	expiresAt := time.Now().Add(as.refreshTTL)
	userToken := types.UserToken{
		ID:           uuid.New(),
		UserID:       user.ID,
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    expiresAt,
	}
	if _, err := as.userTokenRepo.Create(dbc, []*types.UserToken{&userToken}); err != nil {
		as.log.Warn("Create User Token Error", "error", err)
		return "", "", fmt.Errorf("create user token error: %w", err)
	}
	return accessToken, refreshToken, nil
}

func (as *authService) generateAccessToken(dbc dbctx.Context, user *types.User) (string, error) {
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

	foundTokens, ftErr := as.userTokenRepo.GetByAccessTokens(dbctx.Context{Ctx: ctx}, []string{tokenString})
	if ftErr != nil {
		as.log.Warn("Error fetching user token by access token", "error", ftErr)
		return ctx, fmt.Errorf("failed to fetch user token by access token: %w", ftErr)
	}
	if len(foundTokens) == 0 || foundTokens[0] == nil {
		return ctx, fmt.Errorf("user token not found for access token")
	}
	existingToken := foundTokens[0]

	rd := &ctxutil.RequestData{
		TokenString:  tokenString,
		RefreshToken: existingToken.RefreshToken,
		UserID:       userID,
		SessionID:    existingToken.ID, // this token = this session
	}
	ctx = ctxutil.WithRequestData(ctx, rd)
	return ctx, nil
}

func (as *authService) GetAccessTTL() time.Duration {
	return as.accessTTL
}

func randomSecret(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return uuid.New().String()
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func randomNonce(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// fallback
		sum := sha256.Sum256([]byte(uuid.New().String()))
		return base64.RawURLEncoding.EncodeToString(sum[:])
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
