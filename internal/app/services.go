package app

import (
	"fmt"

	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/jobs"
	pipelines "github.com/yungbote/neurobridge-backend/internal/jobs/pipeline"
	"github.com/yungbote/neurobridge-backend/internal/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
	"github.com/yungbote/neurobridge-backend/internal/sse"
)

type Services struct {
	// Core
	Bucket services.BucketService
	OpenAI services.OpenAIClient
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

	// Providers (hard way)
	Vision   services.VisionProviderService
	DocAI    services.DocumentProviderService
	Speech   services.SpeechProviderService
	VideoAI  services.VideoIntelligenceProviderService

	// Local tooling + captioning
	MediaTools services.MediaToolsService
	Caption    services.CaptionProviderService

	// Orchestrator
	ContentExtractor services.ContentExtractionService

	// Job infra
	JobRegistry *jobs.Registry
	JobWorker   *jobs.Worker
}

func wireServices(db *gorm.DB, log *logger.Logger, cfg Config, repos Repos, sseHub *sse.SSEHub) (Services, error) {
	log.Info("Wiring services...")

	bucketService, err := services.NewBucketService(log)
	if err != nil {
		return Services{}, fmt.Errorf("init bucket service: %w", err)
	}

	openaiClient, err := services.NewOpenAIClient(log)
	if err != nil {
		return Services{}, fmt.Errorf("init openai client: %w", err)
	}

	avatarService, err := services.NewAvatarService(db, log, repos.User, bucketService)
	if err != nil {
		return Services{}, fmt.Errorf("init avatar service: %w", err)
	}

	fileService := services.NewFileService(db, log, bucketService, repos.MaterialFile)
	authService := services.NewAuthService(db, log, repos.User, avatarService, repos.UserToken, cfg.JWTSecretKey, cfg.AccessTokenTTL, cfg.RefreshTokenTTL)
	userService := services.NewUserService(db, log, repos.User)
	materialService := services.NewMaterialService(db, log, repos.MaterialSet, repos.MaterialFile, fileService)
	courseService := services.NewCourseService(db, log, repos.Course, repos.MaterialSet)
	moduleService := services.NewModuleService(db, log, repos.Course, repos.CourseModule)
	lessonService := services.NewLessonService(db, log, repos.Course, repos.CourseModule, repos.Lesson)

	// Job infra
	jobNotifier := services.NewJobNotifier(sseHub)
	jobService := services.NewJobService(db, log, repos.JobRun, jobNotifier)
	workflow := services.NewWorkflowService(db, log, materialService, repos.Course, jobService)

	// Course-domain notifier
	courseNotifier := services.NewCourseNotifier(sseHub)

	// ---------- Provider services ----------
	visionProvider, err := services.NewVisionProviderService(log)
	if err != nil {
		return Services{}, fmt.Errorf("init vision provider: %w", err)
	}

	docProvider, err := services.NewDocumentProviderService(log)
	if err != nil {
		return Services{}, fmt.Errorf("init document provider: %w", err)
	}

	speechProvider, err := services.NewSpeechProviderService(log)
	if err != nil {
		return Services{}, fmt.Errorf("init speech provider: %w", err)
	}

	videoProvider, err := services.NewVideoIntelligenceProviderService(log)
	if err != nil {
		return Services{}, fmt.Errorf("init video intelligence provider: %w", err)
	}

	mediaTools := services.NewMediaToolsService(log)

	captionProvider, err := services.NewCaptionProviderService(log, openaiClient)
	if err != nil {
		return Services{}, fmt.Errorf("init caption provider: %w", err)
	}

	// Orchestrator: full extraction pipeline
	extractor := services.NewContentExtractionService(
		db,
		log,
		repos.MaterialChunk,
		repos.MaterialFile,
		bucketService,
		mediaTools,
		docProvider,
		visionProvider,
		speechProvider,
		videoProvider,
		captionProvider,
	)

	// ---------- Job registry ----------
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
		bucketService,
		openaiClient,
		courseNotifier,
		extractor, // <-- NEW: pass the orchestrator into the pipeline
	)
	if err := reg.Register(courseBuild); err != nil {
		return Services{}, err
	}

	worker := jobs.NewWorker(db, log, repos.JobRun, reg, jobNotifier)

	return Services{
		Bucket: bucketService,
		OpenAI: openaiClient,
		Avatar: avatarService,
		File:   fileService,

		Auth:     authService,
		User:     userService,
		Material: materialService,
		Course:   courseService,
		Module:   moduleService,
		Lesson:   lessonService,

		JobNotifier:    jobNotifier,
		JobService:     jobService,
		Workflow:       workflow,
		CourseNotifier: courseNotifier,

		Vision:  visionProvider,
		DocAI:   docProvider,
		Speech:  speechProvider,
		VideoAI: videoProvider,

		MediaTools: mediaTools,
		Caption:    captionProvider,

		ContentExtractor: extractor,

		JobRegistry: reg,
		JobWorker:   worker,
	}, nil
}










