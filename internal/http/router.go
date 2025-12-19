package http

import (
	"github.com/gin-gonic/gin"
	httpH "github.com/yungbote/neurobridge-backend/internal/http/handlers"
	httpMW "github.com/yungbote/neurobridge-backend/internal/http/middleware"
)

type RouterConfig struct {
	AuthHandler     *httpH.AuthHandler
	AuthMiddleware  *httpMW.AuthMiddleware
	UserHandler     *httpH.UserHandler
	RealtimeHandler *httpH.RealtimeHandler

	MaterialHandler *httpH.MaterialHandler
	PathHandler     *httpH.PathHandler
	ActivityHandler *httpH.ActivityHandler
	CourseHandler   *httpH.CourseHandler
	ModuleHandler   *httpH.ModuleHandler
	LessonHandler   *httpH.LessonHandler
	EventHandler    *httpH.EventHandler
	JobHandler      *httpH.JobHandler

	HealthHandler *httpH.HealthHandler
}

func NewRouter(cfg RouterConfig) *gin.Engine {
	r := gin.Default()
	r.Use(httpMW.AttachRequestContext())
	r.Use(httpMW.CORS())

	// Health
	if cfg.HealthHandler != nil {
		r.GET("/healthcheck", cfg.HealthHandler.HealthCheck)
	}

	api := r.Group("/api")
	{
		// Auth (public)
		if cfg.AuthHandler != nil {
			api.POST("/register", cfg.AuthHandler.Register)
			api.POST("/login", cfg.AuthHandler.Login)
		}
	}

	protected := api.Group("/")
	{
		// Middleware
		if cfg.AuthMiddleware != nil {
			protected.Use(cfg.AuthMiddleware.RequireAuth())
		}

		// Auth (protected)
		if cfg.AuthHandler != nil {
			protected.POST("/refresh", cfg.AuthHandler.Refresh)
			protected.POST("/logout", cfg.AuthHandler.Logout)
		}

		// Realtime (SSE)
		if cfg.RealtimeHandler != nil {
			protected.GET("/sse/stream", cfg.RealtimeHandler.SSEStream)
			protected.POST("/sse/subsceribe", cfg.RealtimeHandler.SSESubscribe)
			protected.POST("/sse/unsubscribe", cfg.RealtimeHandler.SSEUnsubscribe)
		}

		// User (Me)
		if cfg.UserHandler != nil {
			protected.GET("/me", cfg.UserHandler.GetMe)
		}

		// Materials
		if cfg.MaterialHandler != nil {
			protected.POST("/material-sets/upload", cfg.MaterialHandler.UploadMaterials)
		}

		// Paths (Path-centric learning)
		if cfg.PathHandler != nil {
			protected.GET("/paths", cfg.PathHandler.ListUserPaths)
			protected.GET("/paths/:id", cfg.PathHandler.GetPath)
			protected.GET("/paths/:id/nodes", cfg.PathHandler.ListPathNodes)
			protected.GET("/path-nodes/:id/activities", cfg.PathHandler.ListPathNodeActivities)
		}

		if cfg.ActivityHandler != nil {
			protected.GET("/activities/:id", cfg.ActivityHandler.GetActivity)
		}

		// Course
		if cfg.CourseHandler != nil {
			protected.GET("/courses", cfg.CourseHandler.ListUserCourses)
		}

		// Module
		if cfg.ModuleHandler != nil {
			protected.GET("/courses/:id/modules", cfg.ModuleHandler.ListCourseModules)
		}

		// Lesson
		if cfg.LessonHandler != nil {
			protected.GET("modules/:id/lessons", cfg.LessonHandler.ListModuleLessons)
			protected.GET("/lessons/:id", cfg.LessonHandler.GetLesson)
		}

		// User Event
		if cfg.EventHandler != nil {
			protected.POST("/events", cfg.EventHandler.Ingest)
		}

		// Job
		if cfg.JobHandler != nil {
			protected.GET("/jobs/:id", cfg.JobHandler.GetJob)
		}
	}

	return r
}
