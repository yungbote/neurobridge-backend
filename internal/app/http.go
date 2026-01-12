package app

import (
	"github.com/gin-gonic/gin"
	"github.com/yungbote/neurobridge-backend/internal/http"
	httpH "github.com/yungbote/neurobridge-backend/internal/http/handlers"
	httpMW "github.com/yungbote/neurobridge-backend/internal/http/middleware"
	learningmod "github.com/yungbote/neurobridge-backend/internal/modules/learning"
	librarymod "github.com/yungbote/neurobridge-backend/internal/modules/library"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/realtime"
	"gorm.io/gorm"
)

type Middleware struct {
	Auth *httpMW.AuthMiddleware
}

type Handlers struct {
	Health   *httpH.HealthHandler
	Auth     *httpH.AuthHandler
	User     *httpH.UserHandler
	Session  *httpH.SessionStateHandler
	Realtime *httpH.RealtimeHandler
	Material *httpH.MaterialHandler
	Chat     *httpH.ChatHandler
	Library  *httpH.LibraryHandler
	Path     *httpH.PathHandler
	Activity *httpH.ActivityHandler
	Event    *httpH.EventHandler
	Job      *httpH.JobHandler
}

func wireHandlers(log *logger.Logger, db *gorm.DB, services Services, repos Repos, clients Clients, sseHub *realtime.SSEHub) Handlers {
	log.Info("Wiring handlers...")
	learningUC := learningmod.New(learningmod.UsecasesDeps{
		DB:        db,
		Log:       log.With("module", "learning"),
		AI:        clients.OpenaiClient,
		Avatar:    services.Avatar,
		Path:      repos.Path,
		PathNodes: repos.PathNode,
		NodeDocs:  repos.LearningNodeDoc,
		Chunks:    repos.MaterialChunk,
		Drills:    repos.DrillInstance,
		GenRuns:   repos.DocGenerationRun,
	})
	libraryUC := librarymod.New(librarymod.UsecasesDeps{
		DB:         db,
		Log:        log.With("module", "library"),
		Jobs:       services.JobService,
		JobRuns:    repos.JobRun,
		TaxNodes:   repos.LibraryTaxonomyNode,
		TaxEdges:   repos.LibraryTaxonomyEdge,
		Membership: repos.LibraryTaxonomyMember,
		State:      repos.LibraryTaxonomyState,
		Snapshots:  repos.LibraryTaxonomySnapshot,
	})
	return Handlers{
		Health:   httpH.NewHealthHandler(),
		Auth:     httpH.NewAuthHandler(services.Auth),
		User:     httpH.NewUserHandler(services.User, sseHub),
		Session:  httpH.NewSessionStateHandler(services.SessionState),
		Realtime: httpH.NewRealtimeHandler(log, sseHub),
		Material: httpH.NewMaterialHandler(
			log,
			services.Workflow,
			sseHub,
			clients.GcpBucket,
			repos.MaterialFile,
			repos.MaterialAsset,
			repos.UserLibraryIndex,
		),
		Chat: httpH.NewChatHandler(services.Chat),
		Library: httpH.NewLibraryHandler(
			log,
			db,
			libraryUC,
			repos.LibraryTaxonomyNode,
			repos.LibraryTaxonomyEdge,
			repos.LibraryTaxonomyMember,
			repos.LibraryTaxonomyState,
			repos.LibraryTaxonomySnapshot,
		),
		Path: httpH.NewPathHandler(
			log,
			db,
			repos.Path,
			repos.PathNode,
			repos.PathNodeActivity,
			repos.Activity,
			repos.LearningNodeDoc,
			repos.LearningNodeDocRevision,
			repos.MaterialChunk,
			repos.MaterialSet,
			repos.MaterialFile,
			repos.MaterialAsset,
			repos.UserLibraryIndex,
			repos.Concept,
			repos.ConceptEdge,
			repos.Asset,
			repos.JobRun,
			services.JobService,
			services.Events,
			services.Avatar,
			learningUC,
			clients.GcpBucket,
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
		SessionHandler:  handlers.Session,
		RealtimeHandler: handlers.Realtime,
		MaterialHandler: handlers.Material,
		ChatHandler:     handlers.Chat,
		LibraryHandler:  handlers.Library,
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
