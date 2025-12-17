package app

import (
	"fmt"
	"os"
	"strings"

	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/jobs"
	pipelines "github.com/yungbote/neurobridge-backend/internal/jobs/pipeline"
	"github.com/yungbote/neurobridge-backend/internal/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
	"github.com/yungbote/neurobridge-backend/internal/sse"
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

	// Jobs + notifications
	JobNotifier    services.JobNotifier
	JobService     services.JobService
	Workflow       services.WorkflowService
	CourseNotifier services.CourseNotifier

	// Local tooling + captioning
	MediaTools services.MediaToolsService

	// Orchestrator
	ContentExtractor services.ContentExtractionService

	// Job infra
	JobRegistry *jobs.Registry
	JobWorker   *jobs.Worker

	// Keep bus here for convenience/compat
	SSEBus services.SSEBus
}

func wireServices(db *gorm.DB, log *logger.Logger, cfg Config, repos Repos, sseHub *sse.SSEHub, clients Clients) (Services, error) {
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

	mediaTools := services.NewMediaToolsService(log)

	extractor := services.NewContentExtractionService(
		db,
		log,
		repos.MaterialChunk,
		repos.MaterialFile,
		clients.GcpBucket,
		mediaTools,
		clients.GcpDocument,
		clients.GcpVision,
		clients.GcpSpeech,
		clients.GcpVideo,
		clients.OpenaiCaption,
	)

	// Job registry + worker
	reg := jobs.NewRegistry()

	courseBuild := pipelines.NewCourseBuildPipeline(
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
	)
	if err := reg.Register(courseBuild); err != nil {
		return Services{}, err
	}

	var worker *jobs.Worker
	if runWorker {
		worker = jobs.NewWorker(db, log, repos.JobRun, reg, jobNotifier)
	}

	return Services{
		Avatar:          avatarService,
		File:            fileService,
		Auth:            authService,
		User:            userService,
		Material:        materialService,
		Course:          courseService,
		Module:          moduleService,
		Lesson:          lessonService,
		JobNotifier:     jobNotifier,
		JobService:      jobService,
		Workflow:        workflow,
		CourseNotifier:  courseNotifier,
		MediaTools:      mediaTools,
		ContentExtractor: extractor,
		JobRegistry:     reg,
		JobWorker:       worker,
		SSEBus:          clients.SSEBus,
	}, nil
}










