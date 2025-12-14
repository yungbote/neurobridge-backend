package app

import (
	"github.com/yungbote/neurobridge-backend/internal/logger"
	"github.com/yungbote/neurobridge-backend/internal/middleware"
)

type Middleware struct {
	Auth										*middleware.AuthMiddleware
}

func wireMiddleware(log *logger.Logger, services Services) Middleware {
	log.Info("Wiring middleware...")
	return Middleware{
		Auth:									middleware.NewAuthMiddleware(log, services.Auth),
	}
}










