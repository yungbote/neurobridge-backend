package app

import (
	"github.com/gin-gonic/gin"
	"github.com/yungbote/neurobridge-backend/internal/http"
	httpH "github.com/yungbote/neurobridge-backend/internal/http/handlers"
	httpMW "github.com/yungbote/neurobridge-backend/internal/http/middleware"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"github.com/yungbote/neurobridge-backend/internal/realtime"
)

type Middleware struct {
	Auth *httpMW.AuthMiddleware
}

type Handlers struct {
	Health   *httpH.HealthHandler
	Auth     *httpH.AuthHandler
	User     *httpH.UserHandler
	Realtime *httpH.RealtimeHandler
	Material *httpH.MaterialHandler
	Chat     *httpH.ChatHandler
	Path     *httpH.PathHandler
	Activity *httpH.ActivityHandler
	Event    *httpH.EventHandler
	Job      *httpH.JobHandler
}

func wireHandlers(log *logger.Logger, services Services, repos Repos, clients Clients, sseHub *realtime.SSEHub) Handlers {
	log.Info("Wiring handlers...")
	return Handlers{
		Health:   httpH.NewHealthHandler(),
		Auth:     httpH.NewAuthHandler(services.Auth),
		User:     httpH.NewUserHandler(services.User, sseHub),
		Realtime: httpH.NewRealtimeHandler(log, sseHub),
		Material: httpH.NewMaterialHandler(log, services.Workflow, sseHub),
		Chat:     httpH.NewChatHandler(services.Chat),
		Path: httpH.NewPathHandler(
			log,
			repos.Path,
			repos.PathNode,
			repos.PathNodeActivity,
			repos.Activity,
			repos.LearningNodeDoc,
			repos.LearningNodeDocRevision,
			repos.DrillInstance,
			repos.DocGenerationRun,
			repos.MaterialChunk,
			repos.MaterialFile,
			repos.Concept,
			repos.ConceptEdge,
			repos.JobRun,
			services.JobService,
			repos.UserProfileVector,
			clients.OpenaiClient,
		),
		Activity: httpH.NewActivityHandler(log, repos.Path, repos.PathNode, repos.PathNodeActivity, repos.Activity),
		Event:    httpH.NewEventHandler(services.Events, services.JobService),
		Job:      httpH.NewJobHandler(services.JobService),
	}
}

func wireRouter(handlers Handlers, middleware Middleware) *gin.Engine {
	return http.NewRouter(http.RouterConfig{
		HealthHandler:   handlers.Health,
		AuthHandler:     handlers.Auth,
		AuthMiddleware:  middleware.Auth,
		UserHandler:     handlers.User,
		RealtimeHandler: handlers.Realtime,
		MaterialHandler: handlers.Material,
		ChatHandler:     handlers.Chat,
		PathHandler:     handlers.Path,
		ActivityHandler: handlers.Activity,
		EventHandler:    handlers.Event,
		JobHandler:      handlers.Job,
	})
}

func wireMiddleware(log *logger.Logger, services Services) Middleware {
	log.Info("Wiring middleware...")
	return Middleware{
		Auth: httpMW.NewAuthMiddleware(log, services.Auth),
	}
}
