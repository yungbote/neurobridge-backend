package app

import (
	"fmt"
	"os"
	"strings"

	"gorm.io/gorm"

	ingestion "github.com/yungbote/neurobridge-backend/internal/ingestion/pipeline"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/course_build"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/user_model_update"
	jobruntime "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
	jobworker "github.com/yungbote/neurobridge-backend/internal/jobs/worker"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"github.com/yungbote/neurobridge-backend/internal/realtime"
	"github.com/yungbote/neurobridge-backend/internal/realtime/bus"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type Services struct {
	// Core
	Avatar services.AvatarService
	File   services.FileService

	// Auth + domain
	Auth     services.AuthService
	User     services.UserService
	Material services.MaterialService
	Course   services.CourseService
	Module   services.ModuleService
	Lesson   services.LessonService

	// User Event Ingestion (raw user_event log)
	Events services.EventService

	// Jobs + notifications
	JobNotifier    services.JobNotifier
	JobService     services.JobService
	Workflow       services.WorkflowService
	CourseNotifier services.CourseNotifier

	// Orchestrator
	ContentExtractor ingestion.ContentExtractionService

	// Job infra
	JobRegistry *jobruntime.Registry
	JobWorker   *jobworker.Worker

	// Keep bus here for convenience/compat
	SSEBus bus.Bus
}

func wireServices(db *gorm.DB, log *logger.Logger, cfg Config, repos Repos, sseHub *realtime.SSEHub, clients Clients) (Services, error) {
	log.Info("Wiring services...")

	avatarService, err := services.NewAvatarService(db, log, repos.User, clients.GcpBucket)
	if err != nil {
		return Services{}, fmt.Errorf("init avatar service: %w", err)
	}

	fileService := services.NewFileService(db, log, clients.GcpBucket, repos.MaterialFile)

	authService := services.NewAuthService(
		db, log,
		repos.User,
		avatarService,
		repos.UserToken,
		cfg.JWTSecretKey,
		cfg.AccessTokenTTL,
		cfg.RefreshTokenTTL,
	)

	userService := services.NewUserService(db, log, repos.User)
	materialService := services.NewMaterialService(db, log, repos.MaterialSet, repos.MaterialFile, fileService)
	courseService := services.NewCourseService(db, log, repos.Course, repos.MaterialSet)
	moduleService := services.NewModuleService(db, log, repos.Course, repos.CourseModule)
	lessonService := services.NewLessonService(db, log, repos.Course, repos.CourseModule, repos.Lesson)
	eventService := services.NewEventService(db, log, repos.UserEvent)

	runServer := strings.EqualFold(strings.TrimSpace(os.Getenv("RUN_SERVER")), "true")
	runWorker := strings.EqualFold(strings.TrimSpace(os.Getenv("RUN_WORKER")), "true")

	var emitter services.SSEEmitter
	if runServer {
		// API: broadcast locally to connected clients
		emitter = &services.HubEmitter{Hub: sseHub}
	} else {
		// Worker: publish to Redis so API can fan-out to clients
		if clients.SSEBus == nil {
			return Services{}, fmt.Errorf("worker requires REDIS_ADDR to publish SSE events")
		}
		emitter = &services.RedisEmitter{Bus: clients.SSEBus}
	}

	jobNotifier := services.NewJobNotifier(emitter)
	jobService := services.NewJobService(db, log, repos.JobRun, jobNotifier)
	workflow := services.NewWorkflowService(db, log, materialService, repos.Course, jobService)
	courseNotifier := services.NewCourseNotifier(emitter)

	extractor := ingestion.NewContentExtractionService(
		db,
		log,
		repos.MaterialChunk,
		repos.MaterialFile,
		clients.GcpBucket,
		clients.LMTools,
		clients.GcpDocument,
		clients.GcpVision,
		clients.GcpSpeech,
		clients.GcpVideo,
		clients.OpenaiCaption,
	)

	// Job registry
	jobRegistry := jobruntime.NewRegistry()

	courseBuild := course_build.NewCourseBuildPipeline(
		db,
		log,
		repos.Course,
		repos.MaterialFile,
		repos.CourseModule,
		repos.Lesson,
		repos.QuizQuestion,
		repos.CourseBlueprint,
		repos.MaterialChunk,
		clients.GcpBucket,
		clients.OpenaiClient,
		courseNotifier,
		extractor,
		clients.PineconeVectorStore,
	)
	if err := jobRegistry.Register(courseBuild); err != nil {
		return Services{}, err
	}

	userModel := user_model_update.New(
		db,
		log,
		repos.UserEvent,
		repos.UserEventCursor,
		repos.UserConceptState,
		repos.UserStylePreference,
		repos.JobRun,
	)
	if err := jobRegistry.Register(userModel); err != nil {
		return Services{}, err
	}

	var worker *jobworker.Worker
	if runWorker {
		worker = jobworker.NewWorker(db, log, repos.JobRun, jobRegistry, jobNotifier)
	}

	return Services{
		Avatar:           avatarService,
		File:             fileService,
		Auth:             authService,
		User:             userService,
		Material:         materialService,
		Course:           courseService,
		Module:           moduleService,
		Lesson:           lessonService,
		Events:           eventService,
		JobNotifier:      jobNotifier,
		JobService:       jobService,
		Workflow:         workflow,
		CourseNotifier:   courseNotifier,
		ContentExtractor: extractor,
		JobRegistry:      jobRegistry,
		JobWorker:        worker,
		SSEBus:           clients.SSEBus,
	}, nil
}
