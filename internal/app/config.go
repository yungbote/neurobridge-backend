package app

import (
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"github.com/yungbote/neurobridge-backend/internal/utils"
	"time"
)

type Config struct {
	JWTSecretKey    string
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
}

func LoadConfig(log *logger.Logger) Config {
	jwtSecretKey := utils.GetEnv("JWT_SECRET_KEY", "defaultsecret", log)
	accessTokenTTLSeconds := utils.GetEnvAsInt("ACCESS_TOKEN_TTL", 3600, log)
	refreshTokenTTLSeconds := utils.GetEnvAsInt("REFRESH_TOKEN_TTL", 86400, log)
	return Config{
		JWTSecretKey:    jwtSecretKey,
		AccessTokenTTL:  time.Duration(accessTokenTTLSeconds) * time.Second,
		RefreshTokenTTL: time.Duration(refreshTokenTTLSeconds) * time.Second,
	}
}
