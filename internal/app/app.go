package app

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/db"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/prompts"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/steps"
	"github.com/yungbote/neurobridge-backend/internal/observability"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/realtime"
)

type App struct {
	Log    *logger.Logger
	DB     *gorm.DB
	Router *gin.Engine
	Cfg    Config
	Repos  Repos

	Clients  Clients
	Services Services

	SSEHub       *realtime.SSEHub
	Metrics      *observability.Metrics
	otelShutdown func(context.Context) error
	cancel       context.CancelFunc
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
	cfg, err := LoadConfig(log)
	if err != nil {
		log.Sync()
		return nil, fmt.Errorf("load config: %w", err)
	}

	prompts.RegisterAll()

	metrics := observability.Init(log)

	pg, err := db.NewPostgresService(log)
	if err != nil {
		log.Sync()
		return nil, fmt.Errorf("init postgres: %w", err)
	}

	// IMPORTANT: never run migrations concurrently (api + worker will race on indexes).
	runMigrations := strings.EqualFold(strings.TrimSpace(os.Getenv("RUN_MIGRATIONS")), "true")
	if runMigrations {
		if err := pg.AutoMigrateAll(); err != nil {
			log.Sync()
			return nil, fmt.Errorf("postgres automigrate: %w", err)
		}
	} else {
		log.Info("Skipping postgres automigrate (RUN_MIGRATIONS != true)")
	}

	theDB := pg.DB()
	ssehub := realtime.NewSSEHub(log)

	reposet := wireRepos(theDB, log)

	clientSet, err := wireClients(log, cfg)
	if err != nil {
		log.Sync()
		return nil, err
	}

	serviceset, err := wireServices(theDB, log, cfg, reposet, ssehub, clientSet, metrics)
	if err != nil {
		clientSet.Close()
		log.Sync()
		return nil, err
	}

	handlerset := wireHandlers(log, theDB, cfg, serviceset, reposet, clientSet, ssehub)
	middleware := wireMiddleware(log, serviceset, cfg)
	router := wireRouter(log, cfg, handlerset, middleware, metrics)

	return &App{
		Log:      log,
		DB:       theDB,
		Router:   router,
		Cfg:      cfg,
		Repos:    reposet,
		Clients:  clientSet,
		Services: serviceset,
		SSEHub:   ssehub,
		Metrics:  metrics,
	}, nil
}

func (a *App) Start(runServer bool, runWorker bool) error {
	if a == nil || a.cancel != nil {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	if a.otelShutdown == nil {
		serviceName := strings.TrimSpace(os.Getenv("OTEL_SERVICE_NAME"))
		if serviceName == "" {
			switch {
			case runServer && !runWorker:
				serviceName = "neurobridge-api"
			case runWorker && !runServer:
				serviceName = "neurobridge-worker"
			default:
				serviceName = "neurobridge"
			}
		}
		a.otelShutdown = observability.InitOTel(ctx, a.Log, observability.OtelConfig{
			ServiceName: serviceName,
			Environment: strings.TrimSpace(os.Getenv("OTEL_ENVIRONMENT")),
			Version:     strings.TrimSpace(os.Getenv("APP_VERSION")),
		})
	}

	if a.Metrics != nil {
		addr := strings.TrimSpace(os.Getenv("METRICS_ADDR"))
		if addr == "" {
			if port := strings.TrimSpace(os.Getenv("METRICS_PORT")); port != "" {
				addr = ":" + port
			}
		}
		a.Metrics.StartServer(ctx, a.Log, addr)
		a.Metrics.StartPostgresCollector(ctx, a.Log, a.DB)
		a.Metrics.StartRedisCollector(ctx, a.Log, os.Getenv("REDIS_ADDR"))
		a.Metrics.StartJobQueueCollector(ctx, a.Log, a.DB)
		a.Metrics.StartSLOEvaluator(ctx, a.Log)
	}

	// (A) API server: Redis -> Hub forwarder (optional)
	if runServer && a.Clients.SSEBus != nil && a.SSEHub != nil {
		a.Log.Info("Starting Redis SSE forwarder...")
		err := a.Clients.SSEBus.StartForwarder(ctx, func(m realtime.SSEMessage) {
			a.SSEHub.Broadcast(m)
		})
		if err != nil {
			a.Log.Error("Failed to start Redis SSE forwarder", "error", err)
		}
	}

	// (B) Worker container: start worker pool
	if runWorker && a.Services.TemporalWorker != nil {
		if err := a.Services.TemporalWorker.Start(ctx); err != nil {
			a.Log.Error("Failed to start Temporal worker", "error", err)
			return err
		}
	}

	// (C) Background: seed teaching patterns once on startup (non-blocking).
	if runServer {
		go a.seedTeachingPatternsOnStartup(ctx)
	}
	return nil
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
	if a.otelShutdown != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = a.otelShutdown(shutdownCtx)
		cancel()
		a.otelShutdown = nil
	}
	a.Clients.Close()
	if a.Log != nil {
		a.Log.Sync()
	}
}

func (a *App) seedTeachingPatternsOnStartup(ctx context.Context) {
	if a == nil || a.DB == nil || a.Log == nil || a.Repos.Activities.TeachingPattern == nil || a.Clients.OpenaiClient == nil {
		return
	}
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("TEACHING_PATTERNS_SEED_ON_STARTUP")), "true") &&
		strings.TrimSpace(os.Getenv("TEACHING_PATTERNS_SEED_ON_STARTUP")) != "" {
		return
	}
	minCount := int64(10)
	if v := strings.TrimSpace(os.Getenv("TEACHING_PATTERNS_SEED_MIN_COUNT")); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			minCount = n
		}
	}
	if n, err := a.Repos.Activities.TeachingPattern.Count(dbctx.Context{Ctx: ctx}); err == nil && n >= minCount {
		return
	}

	// Prefer most recent user profile doc if available.
	profileDoc := ""
	var up types.UserProfileVector
	_ = a.DB.WithContext(ctx).Order("updated_at desc").Limit(1).Find(&up).Error
	if up.ID != uuid.Nil {
		profileDoc = strings.TrimSpace(up.ProfileDoc)
	}
	if profileDoc == "" {
		profileDoc = strings.TrimSpace(os.Getenv("TEACHING_PATTERNS_SEED_FALLBACK_PROFILE_DOC"))
	}
	if profileDoc == "" {
		profileDoc = "Learner prefers clear, structured explanations with examples, concise summaries, and practice checks. Keep tone professional and encouraging. Use visuals when helpful."
	}

	seedCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	_, err := steps.SeedTeachingPatternsFromDoc(seedCtx, steps.TeachingPatternsSeedDeps{
		DB:       a.DB,
		Log:      a.Log,
		Patterns: a.Repos.Activities.TeachingPattern,
		AI:       a.Clients.OpenaiClient,
		Vec:      a.Clients.PineconeVectorStore,
	}, profileDoc, uuid.Nil)
	if err != nil && a.Log != nil {
		a.Log.Warn("teaching_patterns_seed startup failed (continuing)", "error", err)
	}
}
