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
	Bucket services.BucketService
	OpenAI services.OpenAIClient
	Avatar services.AvatarService
	File   services.FileService

	Auth     services.AuthService
	User     services.UserService
	Material services.MaterialService
	Course   services.CourseService
	Module   services.ModuleService
	Lesson   services.LessonService

	JobNotifier services.JobNotifier
	JobService  services.JobService
	Workflow    services.WorkflowService

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

	// Generic job infra
	jobNotifier := services.NewJobNotifier(sseHub)
	jobService := services.NewJobService(db, log, repos.JobRun, jobNotifier)
	workflow := services.NewWorkflowService(db, log, materialService, repos.Course, jobService)

	reg := jobs.NewRegistry()

	// Register pipelines
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

		JobNotifier: jobNotifier,
		JobService:  jobService,
		Workflow:    workflow,

		JobRegistry: reg,
		JobWorker:   worker,
	}, nil
}










