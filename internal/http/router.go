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
	SessionHandler  *httpH.SessionStateHandler
	RealtimeHandler *httpH.RealtimeHandler

	MaterialHandler *httpH.MaterialHandler
	ChatHandler     *httpH.ChatHandler
	LibraryHandler  *httpH.LibraryHandler
	PathHandler     *httpH.PathHandler
	RuntimeHandler  *httpH.RuntimeStateHandler
	ActivityHandler *httpH.ActivityHandler
	EventHandler    *httpH.EventHandler
	GazeHandler     *httpH.GazeHandler
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
			protected.GET("/user/personalization", cfg.UserHandler.GetPersonalizationPrefs)
			protected.PATCH("/user/personalization", cfg.UserHandler.PatchPersonalizationPrefs)
		}

		// Session (runtime state)
		if cfg.SessionHandler != nil {
			protected.GET("/session/state", cfg.SessionHandler.Get)
			protected.PATCH("/session/state", cfg.SessionHandler.Patch)
		}

		// Materials
		if cfg.MaterialHandler != nil {
			protected.POST("/material-sets/upload", cfg.MaterialHandler.UploadMaterials)
			protected.GET("/material-files", cfg.MaterialHandler.ListUserMaterialFiles)
			protected.GET("/material-files/:id/view", cfg.MaterialHandler.ViewMaterialFile)
			protected.GET("/material-files/:id/thumbnail", cfg.MaterialHandler.ViewMaterialFileThumbnail)
			protected.GET("/material-assets/:id/view", cfg.MaterialHandler.ViewMaterialAsset)
		}

		// Chat
		if cfg.ChatHandler != nil {
			protected.POST("/chat/threads", cfg.ChatHandler.CreateThread)
			protected.GET("/chat/threads", cfg.ChatHandler.ListThreads)
			protected.GET("/chat/threads/:id", cfg.ChatHandler.GetThread)
			protected.GET("/chat/intake/pending", cfg.ChatHandler.ListPendingIntakeQuestions)
			protected.POST("/chat/threads/:id/rebuild", cfg.ChatHandler.RebuildThread)
			protected.DELETE("/chat/threads/:id", cfg.ChatHandler.DeleteThread)
			protected.POST("/chat/threads/:id/messages", cfg.ChatHandler.SendMessage)
			protected.GET("/chat/threads/:id/messages", cfg.ChatHandler.ListMessages)
			protected.PATCH("/chat/threads/:id/messages/:message_id", cfg.ChatHandler.UpdateMessage)
			protected.DELETE("/chat/threads/:id/messages/:message_id", cfg.ChatHandler.DeleteMessage)

			protected.GET("/chat/turns/:id", cfg.ChatHandler.GetTurn)
		}

		// Library (taxonomy snapshot)
		if cfg.LibraryHandler != nil {
			protected.GET("/library/taxonomy", cfg.LibraryHandler.GetTaxonomySnapshot)
			protected.GET("/library/taxonomy/nodes/:id/items", cfg.LibraryHandler.ListTaxonomyNodeItems)
		}

		// Paths (Path-centric learning)
		if cfg.PathHandler != nil {
			protected.GET("/paths", cfg.PathHandler.ListUserPaths)
			protected.GET("/paths/:id", cfg.PathHandler.GetPath)
			protected.DELETE("/paths/:id", cfg.PathHandler.DeletePath)
			protected.POST("/paths/:id/view", cfg.PathHandler.ViewPath)
			protected.POST("/paths/:id/cover", cfg.PathHandler.GeneratePathCover)
			protected.GET("/paths/:id/materials", cfg.PathHandler.ListPathMaterials)
			protected.GET("/paths/:id/nodes", cfg.PathHandler.ListPathNodes)
			protected.GET("/paths/:id/concept-graph", cfg.PathHandler.GetConceptGraph)
			protected.GET("/path-nodes/:id/activities", cfg.PathHandler.ListPathNodeActivities)
			protected.GET("/path-nodes/:id/content", cfg.PathHandler.GetPathNodeContent)
			protected.GET("/path-nodes/:id/doc", cfg.PathHandler.GetPathNodeDoc)
			protected.GET("/path-nodes/:id/assets/view", cfg.PathHandler.ViewPathNodeAsset)
			protected.POST("/path-nodes/:id/doc/patch", cfg.PathHandler.EnqueuePathNodeDocPatch)
			protected.GET("/path-nodes/:id/doc/revisions", cfg.PathHandler.ListPathNodeDocRevisions)
			protected.GET("/path-nodes/:id/doc/materials", cfg.PathHandler.ListPathNodeDocMaterials)
			protected.GET("/path-nodes/:id/drills", cfg.PathHandler.ListPathNodeDrills)
			protected.POST("/path-nodes/:id/drills/:kind", cfg.PathHandler.GeneratePathNodeDrill)
			protected.POST("/path-nodes/:id/quick-checks/:block_id/attempt", cfg.PathHandler.AttemptPathNodeQuickCheck)
		}

		// Runtime state
		if cfg.RuntimeHandler != nil {
			protected.GET("/paths/:id/runtime", cfg.RuntimeHandler.GetPathRuntime)
		}

		if cfg.ActivityHandler != nil {
			protected.GET("/activities/:id", cfg.ActivityHandler.GetActivity)
		}

		// User Event
		if cfg.EventHandler != nil {
			protected.POST("/events", cfg.EventHandler.Ingest)
		}

		// Gaze
		if cfg.GazeHandler != nil {
			protected.POST("/gaze/ingest", cfg.GazeHandler.Ingest)
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
