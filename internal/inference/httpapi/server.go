package httpapi

import (
	"net/http"

	"github.com/yungbote/neurobridge-backend/internal/inference/config"
	oaicompat "github.com/yungbote/neurobridge-backend/internal/inference/httpapi/compat/oai"
	apiv1 "github.com/yungbote/neurobridge-backend/internal/inference/httpapi/v1"
	"github.com/yungbote/neurobridge-backend/internal/inference/router"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

func NewServer(cfg *config.Config, log *logger.Logger, r *router.Router) *http.Server {
	h := NewHandler(cfg, log, r)

	return &http.Server{
		Addr:              cfg.HTTP.Addr,
		Handler:           h,
		ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout.Duration,
		IdleTimeout:       cfg.HTTP.IdleTimeout.Duration,
		WriteTimeout:      0,
	}
}

func NewHandler(cfg *config.Config, log *logger.Logger, r *router.Router) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", handleHealthz)
	mux.HandleFunc("GET /readyz", handleReadyz)

	apiv1.Register(mux, cfg, log, r)
	if cfg.HTTP.EnableOAICompat {
		oaicompat.Register(mux, cfg, log, r)
	}

	var h http.Handler = mux
	h = recoverMiddleware(log)(h)
	h = accessLogMiddleware(log)(h)
	h = requestIDMiddleware()(h)

	return h
}
