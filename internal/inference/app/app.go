package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/yungbote/neurobridge-backend/internal/inference/config"
	"github.com/yungbote/neurobridge-backend/internal/inference/httpapi"
	"github.com/yungbote/neurobridge-backend/internal/inference/router"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type App struct {
	Log    *logger.Logger
	Config *config.Config

	server *http.Server
}

func New() (*App, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}

	log, err := logger.New(cfg.Env)
	if err != nil {
		return nil, fmt.Errorf("init logger: %w", err)
	}

	r, err := router.New(cfg)
	if err != nil {
		return nil, err
	}

	srv := httpapi.NewServer(cfg, log, r)

	return &App{
		Log:    log,
		Config: cfg,
		server: srv,
	}, nil
}

func (a *App) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		errCh <- a.server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), a.Config.HTTP.ShutdownTimeout.Duration)
		defer cancel()
		_ = a.server.Shutdown(shutdownCtx)
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
