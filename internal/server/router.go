package server

import (
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"

	"github.com/yungbote/neurobridge-backend/internal/handlers"
	"github.com/yungbote/neurobridge-backend/internal/middleware"
)

type RouterConfig struct {
	AuthHandler    *handlers.AuthHandler
	AuthMiddleware *middleware.AuthMiddleware
	UserHandler    *handlers.UserHandler
	SSEHandler     *handlers.SSEHandler

	MaterialHandler *handlers.MaterialHandler
	CourseHandler   *handlers.CourseHandler
	ModuleHandler   *handlers.ModuleHandler
	LessonHandler   *handlers.LessonHandler

	JobsHandler     *handlers.JobsHandler
}

func NewRouter(cfg RouterConfig) *gin.Engine {
	router := gin.Default()

	// Always attach request-scoped context helpers (SSEData, etc)
	router.Use(middleware.AttachRequestContext())

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

	router.GET("/healthcheck", handlers.HealthCheck)

	api := router.Group("/api")
	{
		api.POST("/register", cfg.AuthHandler.Register)
		api.POST("/login", cfg.AuthHandler.Login)
	}

	protected := api.Group("/")
	protected.Use(cfg.AuthMiddleware.RequireAuth())

	protected.POST("/refresh", cfg.AuthHandler.Refresh)
	protected.POST("/logout", cfg.AuthHandler.Logout)

	protected.GET("/sse/stream", cfg.SSEHandler.SSEStream)
	protected.POST("/sse/subscribe", cfg.SSEHandler.SSESubscribe)
	protected.POST("/sse/unsubscribe", cfg.SSEHandler.SSEUnsubscribe)

	protected.GET("/me", cfg.UserHandler.GetMe)

	if cfg.MaterialHandler != nil {
		protected.POST("/material-sets/upload", cfg.MaterialHandler.UploadMaterials)
	}
	if cfg.CourseHandler != nil {
		protected.GET("/courses", cfg.CourseHandler.ListUserCourses)
	}
	if cfg.ModuleHandler != nil {
		protected.GET("/courses/:id/modules", cfg.ModuleHandler.ListModulesForCourse)
	}
	if cfg.LessonHandler != nil {
		protected.GET("/modules/:id/lessons", cfg.LessonHandler.ListLessonsForModule)
	}

	// Generic job APIs
	if cfg.JobsHandler != nil {
		protected.GET("/jobs/:id", cfg.JobsHandler.GetJobByID)
	}

	return router
}










