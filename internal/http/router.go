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
	ChatHandler     *httpH.ChatHandler
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
			api.POST("/oauth/nonce", cfg.AuthHandler.OAuthNonce)
			api.POST("/oauth/google", cfg.AuthHandler.OAuthGoogle)
			api.POST("/oauth/apple", cfg.AuthHandler.OAuthApple)
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
			// Correct spelling (keep legacy typo route for backwards-compat).
			protected.POST("/sse/subscribe", cfg.RealtimeHandler.SSESubscribe)
			protected.POST("/sse/subsceribe", cfg.RealtimeHandler.SSESubscribe)
			protected.POST("/sse/unsubscribe", cfg.RealtimeHandler.SSEUnsubscribe)
		}

		// User (Me)
		if cfg.UserHandler != nil {
			protected.GET("/me", cfg.UserHandler.GetMe)
			protected.PATCH("/user/name", cfg.UserHandler.ChangeName)
			protected.PATCH("/user/theme", cfg.UserHandler.ChangeTheme)
			protected.PATCH("/user/avatar_color", cfg.UserHandler.ChangeAvatarColor)
			protected.POST("/user/avatar/upload", cfg.UserHandler.UploadAvatar)
		}

		// Materials
		if cfg.MaterialHandler != nil {
			protected.POST("/material-sets/upload", cfg.MaterialHandler.UploadMaterials)
		}

		// Chat
		if cfg.ChatHandler != nil {
			protected.POST("/chat/threads", cfg.ChatHandler.CreateThread)
			protected.GET("/chat/threads", cfg.ChatHandler.ListThreads)
			protected.GET("/chat/threads/:id", cfg.ChatHandler.GetThread)
			protected.POST("/chat/threads/:id/rebuild", cfg.ChatHandler.RebuildThread)
			protected.DELETE("/chat/threads/:id", cfg.ChatHandler.DeleteThread)
			protected.POST("/chat/threads/:id/messages", cfg.ChatHandler.SendMessage)
			protected.GET("/chat/threads/:id/messages", cfg.ChatHandler.ListMessages)
			protected.PATCH("/chat/threads/:id/messages/:message_id", cfg.ChatHandler.UpdateMessage)
			protected.DELETE("/chat/threads/:id/messages/:message_id", cfg.ChatHandler.DeleteMessage)

			protected.GET("/chat/turns/:id", cfg.ChatHandler.GetTurn)
		}

		// Paths (Path-centric learning)
		if cfg.PathHandler != nil {
			protected.GET("/paths", cfg.PathHandler.ListUserPaths)
			protected.GET("/paths/:id", cfg.PathHandler.GetPath)
			protected.GET("/paths/:id/nodes", cfg.PathHandler.ListPathNodes)
			protected.GET("/paths/:id/concept-graph", cfg.PathHandler.GetConceptGraph)
			protected.GET("/path-nodes/:id/activities", cfg.PathHandler.ListPathNodeActivities)
			protected.GET("/path-nodes/:id/content", cfg.PathHandler.GetPathNodeContent)
			protected.GET("/path-nodes/:id/doc", cfg.PathHandler.GetPathNodeDoc)
			protected.GET("/path-nodes/:id/drills", cfg.PathHandler.ListPathNodeDrills)
			protected.POST("/path-nodes/:id/drills/:kind", cfg.PathHandler.GeneratePathNodeDrill)
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
			protected.POST("/jobs/:id/cancel", cfg.JobHandler.CancelJob)
			protected.POST("/jobs/:id/restart", cfg.JobHandler.RestartJob)
		}
	}

	return r
}
