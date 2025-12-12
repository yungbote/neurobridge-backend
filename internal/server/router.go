package server

import (
  "github.com/gin-gonic/gin"
  "github.com/gin-contrib/cors"
  "github.com/yungbote/neurobridge-backend/internal/handlers"
  "github.com/yungbote/neurobridge-backend/internal/middleware"
)

type RouterConfig struct {
  AuthHandler           *handlers.AuthHandler
  AuthMiddleware        *middleware.AuthMiddleware
  UserHandler           *handlers.UserHandler
  SSEHandler            *handlers.SSEHandler
//  UserProfileHandler    *handlers.UserProfileHandler
  MaterialHandler       *handlers.MaterialHandler
  CourseHandler         *handlers.CourseHandler
  CourseGenHandler      *handlers.CourseGenHandler
  ModuleHandler         *handlers.ModuleHandler
  LessonHandler         *handlers.LessonHandler
//  PipelineHandler       *handlers.PipelineHandler
//  CourseHandler         *handlers.CourseHandler
//  LessonHandler         *handlers.LessonHandler
//  QuizHandler           *handlers.QuizHandler
//  TelemetryHandler      *handlers.TelemetryHandler
//  RuntimeHandler        *handlers.RuntimeHandler
//  RecommendationHandler *handlers.RecommendationHandler
}

func NewRouter(cfg RouterConfig) *gin.Engine {
  router := gin.Default()

  // Cors
  router.Use(cors.New(cors.Config{
    AllowOrigins: []string{
      "http://localhost:80",
      "http://localhost:3000",
      "http://localhost:5174",
    },
    AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"},
    AllowHeaders:     []string{"Authorization", "Content-Type", "X-Requested-With"},
    AllowCredentials: true,
  }))



// ===============
// || Public    ||
// ===============
  router.GET("/healthcheck", handlers.HealthCheck)
  api := router.Group("/api")
  {
    api.POST("/register", cfg.AuthHandler.Register)
    api.POST("/login", cfg.AuthHandler.Login)
  }

// ===============
// || Protected ||
// ===============
  protected := api.Group("/")
  protected.Use(cfg.AuthMiddleware.RequireAuth())
  // Auth
  protected.POST("/refresh", cfg.AuthHandler.Refresh)
  protected.POST("/logout", cfg.AuthHandler.Logout)
  // SSE
  protected.GET("/sse/stream", cfg.SSEHandler.SSEStream)
  protected.POST("/sse/subscribe", cfg.SSEHandler.SSESubscribe)
  protected.POST("/sse/unsubscribe", cfg.SSEHandler.SSEUnsubscribe)
  // User
  protected.GET("/me", cfg.UserHandler.GetMe)
  // User learning profile & mastery
/*  if cfg.UserProfileHandler != nil {
    protected.GET("/user/learning-profile", cfg.UserProfileHandler.GetLearningProfile)
    protected.PATCH("/user/learning-profile", cfg.UserProfileHandler.UpdateLearningProfile)
    protected.POST("/user/learning-profile/infer", cfg.UserProfileHandler.InferLearningProfile)
    protected.POST("/user/learning-profile/suggest", cfg.UserProfileHandler.SuggestLearningProfileAdjustments)
    protected.GET("/user/topic-mastery", cfg.UserProfileHandler.GetTopicMastery)
  }*/
  // Materials (user-facing)
  if cfg.MaterialHandler != nil {
//    protected.POST("/material-sets/upload-and-generate", cfg.MaterialHandler.UploadAndGenerateCourse)
    protected.POST("/material-sets/upload", cfg.MaterialHandler.UploadMaterials)
//    protected.GET("/material-sets", cfg.MaterialHandler.ListMaterialSets)
//    protected.GET("/material-sets/:id", cfg.MaterialHandler.GetMaterialSet)
//    protected.DELETE("/material-sets/:id", cfg.MaterialHandler.DeleteMaterialSet)
//    protected.GET("/material-sets/:id/files", cfg.MaterialHandler.ListMaterialFiles)
//    protected.GET("/material-files/:id", cfg.MaterialHandler.GetMaterialFile)
//    protected.DELETE("/material-files/:id", cfg.MaterialHandler.DeleteMaterialFile)
  }
  // Pipeline (admin-like control plane; you can mount under /admin)
/*  if cfg.PipelineHandler != nil {
    admin := protected.Group("/admin")
    admin.POST("/material-sets/:id/analyze", cfg.PipelineHandler.AnalyzeMaterialSet)
    admin.POST("/material-sets/:id/plan-course", cfg.PipelineHandler.PlanCourse)
    admin.POST("/material-sets/:id/generate-course", cfg.PipelineHandler.GenerateCourse)
    admin.POST("/material-sets/:id/run-full", cfg.PipelineHandler.RunFullPipeline)
    admin.GET("/material-sets/:id/pipeline-status", cfg.PipelineHandler.GetPipelineStatus)
    admin.POST("/material-sets/:id/cancel", cfg.PipelineHandler.CancelPipeline)
    admin.POST("/courses/:id/generate-lessons", cfg.PipelineHandler.GenerateLessonsForCourse)
    admin.POST("/courses/:id/generate-lesson/:lessonId", cfg.PipelineHandler.RegenerateLesson)
  }*/
  // Courses
  if cfg.CourseHandler != nil {
    protected.GET("/courses", cfg.CourseHandler.ListUserCourses)
//    protected.GET("/courses/:id", cfg.CourseHandler.GetCourse)
//    protected.GET("/courses/:id/outline", cfg.CourseHandler.GetCourseOutline)
//    protected.PATCH("/courses/:id", cfg.CourseHandler.UpdateCourse)
//    protected.POST("/courses/:id/publish", cfg.CourseHandler.PublishCourse)
//    protected.POST("/courses/:id/unpublish", cfg.CourseHandler.UnpublishCourse)
//    protected.POST("/courses/:id/duplicate", cfg.CourseHandler.DuplicateCourse)
//    protected.DELETE("/courses/:id", cfg.CourseHandler.DeleteCourse)
//    protected.GET("/courses/:id/versions", cfg.CourseHandler.ListCourseVersions)
//    protected.GET("/courses/:id/versions/:versionId", cfg.CourseHandler.GetCourseVersion)
  }
  // CourseGen
  if cfg.CourseGenHandler != nil {
    protected.GET("/courses/:id/generation", cfg.CourseGenHandler.GetLatestForCourse)
    protected.GET("/course-generation-runs/:id", cfg.CourseGenHandler.GetRunByID)
  }
  if cfg.ModuleHandler != nil {
    protected.GET("/courses/:id/modules", cfg.ModuleHandler.ListModulesForCourse)
  }
  // Lessons
  if cfg.LessonHandler != nil {
    protected.GET("/modules/:id/lessons", cfg.LessonHandler.ListLessonsForModule)
//    protected.GET("/lessons/:id", cfg.LessonHandler.GetLesson)
//    protected.GET("/courses/:id/lessons", cfg.LessonHandler.ListCourseLessons)
//    protected.PATCH("/lessons/:id", cfg.LessonHandler.UpdateLesson)
//    protected.GET("/lessons/:id/history", cfg.LessonHandler.GetLessonHistory)
//    protected.POST("/lessons/:id/events", cfg.LessonHandler.RecordLessonEvent)
//    protected.POST("/lessons/:id/reorder", cfg.LessonHandler.ReorderLessons)
  }
  // Quiz
/*  if cfg.QuizHandler != nil {
    protected.GET("/lessons/:id/quiz", cfg.QuizHandler.GetLessonQuiz)
    protected.POST("/quiz-attempts", cfg.QuizHandler.SubmitQuizAttempt)
    protected.POST("/lessons/:id/quiz/regenerate", cfg.QuizHandler.RegenerateLessonQuiz)
    protected.GET("/lessons/:id/quiz/history", cfg.QuizHandler.GetLessonQuizHistory)
  }*/
  // Telemetry
/*  if cfg.TelemetryHandler != nil {
    protected.POST("/telemetry/lesson-event", cfg.TelemetryHandler.RecordLessonEvent)
    protected.POST("/telemetry/quiz-attempt", cfg.TelemetryHandler.RecordQuizAttempt)
    protected.POST("/telemetry/feedback", cfg.TelemetryHandler.RecordFeedback)
    protected.POST("/telemetry/batch", cfg.TelemetryHandler.RecordBatch)
  }*/
  // Adaptive runtime
/*  if cfg.RuntimeHandler != nil {
    protected.GET("/runtime/next-lesson", cfg.RuntimeHandler.GetNextLesson)
    protected.POST("/runtime/lesson-view", cfg.RuntimeHandler.GetAdaptedLessonView)
    protected.GET("/runtime/practice-set", cfg.RuntimeHandler.GetPracticeSet)
    protected.GET("/runtime/review-plan", cfg.RuntimeHandler.GetReviewPlan)
    protected.POST("/runtime/preview-adaptation", cfg.RuntimeHandler.PreviewAdaptation)
  }*/
  // Recommendations
/*  if cfg.RecommendationHandler != nil {
    protected.GET("/recommendations/next-steps", cfg.RecommendationHandler.GetNextSteps)
    protected.GET("/recommendations/review", cfg.RecommendationHandler.GetReviewItems)
    protected.GET("/recommendations/resources", cfg.RecommendationHandler.GetRecommendedResources)
  }*/
  return router
}










