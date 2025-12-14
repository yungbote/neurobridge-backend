package app

import (
	"context"
	"fmt"
	"os"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"github.com/yungbote/neurobridge-backend/internal/db"
	"github.com/yungbote/neurobridge-backend/internal/logger"
	"github.com/yungbote/neurobridge-backend/internal/sse"
)

type App struct {
	Log					*logger.Logger
	DB					*gorm.DB
	Router			*gin.Engine
	Cfg					Config
	Repos				Repos
	Services		Services
	SSEHub			*sse.SSEHub
	cancel			context.CancelFunc
}

func New() (*App, error) {
	// Logger
	logMode := os.Getenv("LOG_MODE")
	if logMode == "" {
		logMode = "development"
	}
	log, err := logger.New(logMode)
	if err != nil {
		return nil, fmt.Errorf("init logger: %w", err)
	}

	// Config
	log.Info("Loading environment variables...")
	cfg := LoadConfig(log)
	
	// Postgres
	pg, err := db.NewPostgresService(log)
	if err != nil {
		log.Sync()
		return nil, fmt.Errorf("init postgres: %w", err)
	}
	if err := pg.AutoMigrateAll(); err != nil {
		log.Sync()
		return nil, fmt.Errorf("postgres automigrate: %w", err)
	}
	theDB := pg.DB()

	// SSEHub
	ssehub := sse.NewSSEHub(log)
	// Repos
	reposet := wireRepos(theDB, log)
	// Services
	serviceset, err := wireServices(theDB, log, cfg, reposet, ssehub)
	if err != nil { 
		log.Sync()
		return nil, err 
	}
	// Handlers
	handlerset := wireHandlers(log, serviceset, ssehub)
	// Middleware
	middleware := wireMiddleware(log, serviceset)
	// Router
	router := wireRouter(handlerset, middleware)
	
	// App
	return &App{
		Log:			log,
		DB:				theDB,
		Router:		router,
		Cfg:			cfg,
		Repos:		reposet,
		Services:	serviceset,
		SSEHub:		ssehub,
	}, nil
}

func (a *App) Start() {
	if a == nil || a.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	if a.Services.CourseGeneration != nil {
		a.Services.CourseGeneration.StartWorker(ctx)
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
	if a.Log != nil {
		a.Log.Sync()
	}
}










