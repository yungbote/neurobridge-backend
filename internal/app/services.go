package app

import (
	"fmt"
	"gorm.io/gorm"
	"github.com/yungbote/neurobridge-backend/internal/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
	"github.com/yungbote/neurobridge-backend/internal/sse"
)

type Services struct {
	Bucket									services.BucketService
	OpenAI									services.OpenAIClient
	Avatar									services.AvatarService
	File										services.FileService
	Auth										services.AuthService
	User										services.UserService
	Material								services.MaterialService
	Course									services.CourseService
	CourseGeneration				services.CourseGenerationService
	CourseGenerationStatus	services.CourseGenStatusService
	Module									services.ModuleService
	Lesson									services.LessonService
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
	courseGenerationService := services.NewCourseGenerationService(db, log, sseHub, repos.Course, repos.MaterialSet, repos.MaterialFile, repos.CourseModule, repos.Lesson, repos.QuizQuestion, repos.CourseBlueprint, repos.MaterialChunk, repos.CourseGenerationRun, bucketService, openaiClient)
	courseGenerationStatusService := services.NewCourseGenStatusService(db, repos.CourseGenerationRun, repos.Course)
	moduleService := services.NewModuleService(db, log, repos.Course, repos.CourseModule)
	lessonService := services.NewLessonService(db, log, repos.Course, repos.CourseModule, repos.Lesson)
	return Services{
		Bucket:										bucketService,
		OpenAI:										openaiClient,
		Avatar:										avatarService,
		File:											fileService,
		Auth:											authService,
		User:											userService,
		Material:									materialService,
		Course:										courseService,
		CourseGeneration:					courseGenerationService,
		CourseGenerationStatus:		courseGenerationStatusService,
		Module:										moduleService,
		Lesson:										lessonService,
	}, nil
}










