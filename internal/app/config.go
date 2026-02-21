package app

import (
	"github.com/yungbote/neurobridge-backend/internal/platform/gcp"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/utils"
	"time"
)

type Config struct {
	JWTSecretKey              string
	AccessTokenTTL            time.Duration
	RefreshTokenTTL           time.Duration
	NonceRefreshTTL           time.Duration
	GoogleOIDCClientID        string
	AppleOIDCClientID         string
	ObjectStorageMode         string
	StorageEmulatorHost       string
	StorageModeCompatFallback bool
	VectorProvider            string
	VectorProviderModeSource  string
	QdrantURL                 string
	QdrantCollection          string
	QdrantNamespacePrefix     string
	QdrantVectorDim           int
}

func LoadConfig(log *logger.Logger) (Config, error) {
	jwtSecretKey := utils.GetEnv("JWT_SECRET_KEY", "defaultsecret", log)
	accessTokenTTLSeconds := utils.GetEnvAsInt("ACCESS_TOKEN_TTL", 3600, log)
	refreshTokenTTLSeconds := utils.GetEnvAsInt("REFRESH_TOKEN_TTL", 86400, log)
	nonceRefreshTTLSeconds := utils.GetEnvAsInt("NONCE_REFRESH_TTL", 86400, log)
	googleOIDCClientID := utils.GetEnv("GOOGLE_OIDC_CLIENT_ID", "", log)
	appleOIDCClientID := utils.GetEnv("APPLE_OIDC_CLIENT_ID", "", log)

	storageCfg, err := gcp.ResolveObjectStorageConfigFromEnv()
	if err != nil {
		return Config{}, err
	}
	vectorCfg, err := resolveVectorProviderConfig(storageCfg.Mode)
	if err != nil {
		return Config{}, err
	}

	return Config{
		JWTSecretKey:              jwtSecretKey,
		AccessTokenTTL:            time.Duration(accessTokenTTLSeconds) * time.Second,
		RefreshTokenTTL:           time.Duration(refreshTokenTTLSeconds) * time.Second,
		NonceRefreshTTL:           time.Duration(nonceRefreshTTLSeconds) * time.Second,
		GoogleOIDCClientID:        googleOIDCClientID,
		AppleOIDCClientID:         appleOIDCClientID,
		ObjectStorageMode:         string(storageCfg.Mode),
		StorageEmulatorHost:       storageCfg.EmulatorHost,
		StorageModeCompatFallback: storageCfg.CompatibilityFallback,
		VectorProvider:            string(vectorCfg.Provider),
		VectorProviderModeSource:  vectorCfg.ModeSource,
		QdrantURL:                 vectorCfg.Qdrant.URL,
		QdrantCollection:          vectorCfg.Qdrant.Collection,
		QdrantNamespacePrefix:     vectorCfg.Qdrant.NamespacePrefix,
		QdrantVectorDim:           vectorCfg.Qdrant.VectorDim,
	}, nil
}
