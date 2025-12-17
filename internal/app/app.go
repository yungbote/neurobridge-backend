package app

import (
	"context"
	"fmt"
	"strings"
	"os"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/db"
	"github.com/yungbote/neurobridge-backend/internal/logger"
	"github.com/yungbote/neurobridge-backend/internal/sse"
)

type App struct {
	Log    *logger.Logger
	DB     *gorm.DB
	Router *gin.Engine
	Cfg    Config
	Repos  Repos
	Services Services
	SSEHub *sse.SSEHub
	cancel context.CancelFunc
}

func New() (*App, error) {
	logMode := os.Getenv("LOG_MODE")
	if logMode == "" {
		logMode = "development"
	}
	log, err := logger.New(logMode)
	if err != nil {
		return nil, fmt.Errorf("init logger: %w", err)
	}

	log.Info("Loading environment variables...")
	cfg := LoadConfig(log)

	pg, err := db.NewPostgresService(log)
	if err != nil {	
		log.Sync()
		return nil, fmt.Errorf("init postgres: %w", err)
	}

	// IMPORTANT: never run migrations concurrently (api + worker will race on indexes).
	runMigrations := strings.ToLower(strings.TrimSpace(os.Getenv("RUN_MIGRATIONS"))) == "true"
	if runMigrations {
		if err := pg.AutoMigrateAll(); err != nil {
			log.Sync()
			return nil, fmt.Errorf("postgres automigrate: %w", err)
		}
	} else {
		log.Info("Skipping postgres automigrate (RUN_MIGRATIONS != true)")
	}
	theDB := pg.DB()


	ssehub := sse.NewSSEHub(log)

	reposet := wireRepos(theDB, log)

	serviceset, err := wireServices(theDB, log, cfg, reposet, ssehub)
	if err != nil {
		log.Sync()
		return nil, err
	}

	handlerset := wireHandlers(log, serviceset, ssehub)
	middleware := wireMiddleware(log, serviceset)
	router := wireRouter(handlerset, middleware)

	return &App{
		Log:      log,
		DB:       theDB,
		Router:   router,
		Cfg:      cfg,
		Repos:    reposet,
		Services: serviceset,
		SSEHub:   ssehub,
	}, nil
}

func (a *App) Start(runServer bool, runWorker bool) {
	if a == nil || a.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel

	// (A) If we're the API server, start Redis -> Hub forwarder
	if runServer && a.Services.SSEBus != nil && a.SSEHub != nil {
		a.Log.Info("Starting Redis SSE forwarder...")
		err := a.Services.SSEBus.StartForwarder(ctx, func(m sse.SSEMessage) {
			a.SSEHub.Broadcast(m)
		})
		if err != nil {
			a.Log.Error("Failed to start Redis SSE forwarder", "error", err)
		}
	}

	// (B) If we're the worker container, start worker pool
	if runWorker && a.Services.JobWorker != nil {
		a.Services.JobWorker.Start(ctx)
	}
}

func (a *App) Run(addr string) error {
	if a == nil || a.Router == nil {
		return fmt.Errorf("app not initialized")
	}
	return a.Router.Run(addr)
}

func (a *App) Close() {
	if a == nil {
		return
	}
	if a.cancel != nil {
		a.cancel()
		a.cancel = nil
	}
	if a.Services.SSEBus != nil {
		_ = a.Services.SSEBus.Close()
	}
	if a.Log != nil {
		a.Log.Sync()
	}
}










