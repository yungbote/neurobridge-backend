package main

import (
  "context"
  "fmt"
  "os"
  "time"
  "github.com/yungbote/neurobridge-backend/internal/logger"
  "github.com/yungbote/neurobridge-backend/internal/utils"
  "github.com/yungbote/neurobridge-backend/internal/db"
  "github.com/yungbote/neurobridge-backend/internal/repos"
  "github.com/yungbote/neurobridge-backend/internal/services"
  "github.com/yungbote/neurobridge-backend/internal/handlers"
  "github.com/yungbote/neurobridge-backend/internal/middleware"
  "github.com/yungbote/neurobridge-backend/internal/server"
  "github.com/yungbote/neurobridge-backend/internal/sse"
)

func main() {
  // Logger
  logMode := os.Getenv("LOG_MODE")
  if logMode == "" {
    logMode = "development"
  }
  log, err := logger.New(logMode)
  if err != nil {
    fmt.Printf("Failed to init logger: %v\n", err)
    os.Exit(1)
  }
  defer log.Sync()
  
  // Env
  log.Info("Loading environment variables from main...")
  jwtSecretKey := utils.GetEnv("JWT_SECRET_KEY", "defaultsecret", log)
  accessTokenTTL := utils.GetEnvAsInt("ACCESS_TOKEN_TTL", 3600, log)
  refreshTokenTTL := utils.GetEnvAsInt("REFRESH_TOKEN_TTL", 86400, log)
  
  //Postgres
  postgresService, err := db.NewPostgresService(log)
  if err != nil {
    log.Warn("Postgres init failed", "error", err)
  }
  if err = postgresService.AutoMigrateAll(); err != nil {
    log.Warn("Postgres auto migration failed", "error", err)
  }
  thePG := postgresService.DB()
  
  // Repos
  log.Info("Setting up Repos from main...")
  userRepo := repos.NewUserRepo(thePG, log)
  userTokenRepo := repos.NewUserTokenRepo(thePG, log)
  materialSetRepo := repos.NewMaterialSetRepo(thePG, log)
  materialFileRepo := repos.NewMaterialFileRepo(thePG, log)
  courseRepo := repos.NewCourseRepo(thePG, log)
  courseModuleRepo := repos.NewCourseModuleRepo(thePG, log)
  lessonRepo := repos.NewLessonRepo(thePG, log)
  quizQuestionRepo := repos.NewQuizQuestionRepo(thePG, log)
  courseBlueprintRepo := repos.NewCourseBlueprintRepo(thePG, log)
  lessonAssetRepo := repos.NewLessonAssetRepo(thePG, log)
  learningProfileRepo := repos.NewLearningProfileRepo(thePG, log)
  topicMasteryRepo := repos.NewTopicMasteryRepo(thePG, log)
  lessonProgressRepo := repos.NewLessonProgressRepo(thePG, log)
  quizAttemptRepo := repos.NewQuizAttemptRepo(thePG, log)
  userEventRepo := repos.NewUserEventRepo(thePG, log)
  materialChunkRepo := repos.NewMaterialChunkRepo(thePG, log)
  courseGenRunRepo := repos.NewCourseGenerationRunRepo(thePG, log)

  _ = lessonAssetRepo
  _ = lessonProgressRepo
  _ = learningProfileRepo
  _ = topicMasteryRepo
  _ = quizAttemptRepo
  _ = userEventRepo

  // SSE
  log.Info("Setting up SSE hub now...")
  sseHub := sse.NewSSEHub(log)

  // Services
  log.Info("Setting up Services from main...")
  bucketService, err := services.NewBucketService(log)
  if err != nil {
    log.Warn("Could not init BucketService", "error", err)
  }
  openaiClient, err := services.NewOpenAIClient(log)
  if err != nil {
    log.Error("Could not init OpenAIClient", "error", err)
    os.Exit(1)
  }
  avatarService, err := services.NewAvatarService(thePG, log, userRepo, bucketService)
  if err != nil {
    log.Error("Could not init AvatarService", "error", err)
    os.Exit(1)
  }
  fileService := services.NewFileService(thePG, log, bucketService, materialFileRepo)
  authService := services.NewAuthService(thePG, log, userRepo, avatarService, userTokenRepo, jwtSecretKey, time.Duration(accessTokenTTL)*time.Second, time.Duration(refreshTokenTTL)*time.Second)
  userService := services.NewUserService(thePG, log, userRepo)
  materialService := services.NewMaterialService(thePG, log, materialSetRepo, materialFileRepo, fileService)
  courseService := services.NewCourseService(thePG, log, courseRepo, materialSetRepo)
  courseGenService := services.NewCourseGenerationService(
    thePG,
    log,
    sseHub,
    courseRepo,
    materialSetRepo,
    materialFileRepo,
    courseModuleRepo,
    lessonRepo,
    quizQuestionRepo,
    courseBlueprintRepo,
    materialChunkRepo,
    courseGenRunRepo,
    bucketService,
    openaiClient,
  )
  courseGenService.StartWorker(context.Background())
  courseGenStatusService := services.NewCourseGenStatusService(thePG, courseGenRunRepo, courseRepo)
  moduleService := services.NewModuleService(thePG, log, courseRepo, courseModuleRepo)
  lessonService := services.NewLessonService(thePG, log, courseRepo, courseModuleRepo, lessonRepo)
  courseGenHandler := handlers.NewCourseGenHandler(courseGenStatusService)

  // Handlers
  log.Info("Setting up handlers from main...")
  authHandler := handlers.NewAuthHandler(authService)
  userHandler := handlers.NewUserHandler(userService)
  sseHandler := handlers.NewSSEHandler(log, sseHub)
  materialHandler := handlers.NewMaterialHandler(log, materialService, courseGenService, sseHub)
  courseHandler := handlers.NewCourseHandler(log, courseService)
  moduleHandler := handlers.NewModuleHandler(moduleService)
  lessonHandler := handlers.NewLessonHandler(lessonService)
  // Middleware
  log.Info("Setting up middleware from main...")
  authMiddleware := middleware.NewAuthMiddleware(log, authService)

  // Router
  log.Info("Setting up router from main...")
  router := server.NewRouter(server.RouterConfig{
    AuthHandler:          authHandler,
    AuthMiddleware:       authMiddleware,
    UserHandler:          userHandler,
    SSEHandler:           sseHandler,
    MaterialHandler:      materialHandler,
    CourseHandler:        courseHandler,
    CourseGenHandler:     courseGenHandler,
    ModuleHandler:        moduleHandler,
    LessonHandler:        lessonHandler,
  })

  port := utils.GetEnv("PORT", "8080", log)
  fmt.Printf("Server listening on :%s\n", port)
  if err := router.Run(":" + port); err != nil {
    log.Warn("Server failed: %v", err)
  }
}










