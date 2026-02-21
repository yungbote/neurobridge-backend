package app

import (
	"github.com/gin-gonic/gin"
	"github.com/yungbote/neurobridge-backend/internal/http"
	httpH "github.com/yungbote/neurobridge-backend/internal/http/handlers"
	httpMW "github.com/yungbote/neurobridge-backend/internal/http/middleware"
	learningmod "github.com/yungbote/neurobridge-backend/internal/modules/learning"
	librarymod "github.com/yungbote/neurobridge-backend/internal/modules/library"
	"github.com/yungbote/neurobridge-backend/internal/observability"
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
	Runtime  *httpH.RuntimeStateHandler
	Realtime *httpH.RealtimeHandler
	Material *httpH.MaterialHandler
	Chat     *httpH.ChatHandler
	Library  *httpH.LibraryHandler
	Path     *httpH.PathHandler
	Activity *httpH.ActivityHandler
	Event    *httpH.EventHandler
	Gaze     *httpH.GazeHandler
	Job      *httpH.JobHandler
}

func wireHandlers(log *logger.Logger, db *gorm.DB, cfg Config, services Services, repos Repos, clients Clients, sseHub *realtime.SSEHub) Handlers {
	log.Info("Wiring handlers...")
	learningUC := learningmod.New(learningmod.UsecasesDeps{
		DB:           db,
		Log:          log.With("module", "learning"),
		AI:           clients.OpenaiClient,
		Avatar:       services.Avatar,
		Path:         repos.Paths.Path,
		PathNodes:    repos.Paths.PathNode,
		NodeDocs:     repos.DocGen.LearningNodeDoc,
		Concepts:     repos.Concepts.Concept,
		Chunks:       repos.Materials.MaterialChunk,
		Drills:       repos.Materials.DrillInstance,
		GenRuns:      repos.DocGen.DocGenerationRun,
		ConceptState: repos.Learning.UserConceptState,
	})
	libraryUC := librarymod.New(librarymod.UsecasesDeps{
		DB:         db,
		Log:        log.With("module", "library"),
		Jobs:       services.JobService,
		JobRuns:    repos.Jobs.JobRun,
		TaxNodes:   repos.Library.LibraryTaxonomyNode,
		TaxEdges:   repos.Library.LibraryTaxonomyEdge,
		Membership: repos.Library.LibraryTaxonomyMember,
		State:      repos.Library.LibraryTaxonomyState,
		Snapshots:  repos.Library.LibraryTaxonomySnapshot,
	})

	runtimeHandler := httpH.NewRuntimeStateHandlerWithDeps(httpH.RuntimeStateHandlerDeps{
		Runs: httpH.RuntimeStateRunRepos{
			PathRuns: repos.Paths.PathRun,
			NodeRuns: repos.Paths.NodeRun,
			ActRuns:  repos.Paths.ActivityRun,
		},
		Learning: httpH.RuntimeStateLearningRepos{
			PathNodes:     repos.Paths.PathNode,
			Concepts:      repos.Concepts.Concept,
			ConceptStates: repos.Learning.UserConceptState,
			ConceptModels: repos.Learning.UserConceptModel,
			MisconRepo:    repos.Learning.UserMisconception,
			CalibRepo:     repos.Learning.UserConceptCalibration,
			AlertRepo:     repos.Learning.UserModelAlert,
			ReadinessRepo: repos.Learning.ConceptReadinessSnapshot,
			GateRepo:      repos.Learning.PrereqGateDecision,
		},
	})

	materialHandler := httpH.NewMaterialHandlerWithDeps(httpH.MaterialHandlerDeps{
		Log:      log,
		Workflow: services.Workflow,
		SSEHub:   sseHub,
		Bucket:   clients.GcpBucket,
		Repos: httpH.MaterialHandlerRepoDeps{
			MaterialFiles:    repos.Materials.MaterialFile,
			MaterialAssets:   repos.Materials.MaterialAsset,
			UserLibraryIndex: repos.Library.UserLibraryIndex,
		},
	})

	libraryHandler := httpH.NewLibraryHandlerWithDeps(httpH.LibraryHandlerDeps{
		Log:     log,
		DB:      db,
		Library: libraryUC,
		Repos: httpH.LibraryHandlerRepoDeps{
			Nodes:      repos.Library.LibraryTaxonomyNode,
			Edges:      repos.Library.LibraryTaxonomyEdge,
			Membership: repos.Library.LibraryTaxonomyMember,
			State:      repos.Library.LibraryTaxonomyState,
			Snapshots:  repos.Library.LibraryTaxonomySnapshot,
		},
	})

	pathHandler := httpH.NewPathHandlerWithDeps(httpH.PathHandlerDeps{
		Log: log,
		DB:  db,
		Path: httpH.PathHandlerPathRepos{
			Path:             repos.Paths.Path,
			PathNodes:        repos.Paths.PathNode,
			PathNodeActivity: repos.Paths.PathNodeActivity,
		},
		Content: httpH.PathHandlerContentRepos{
			Activities:         repos.Activities.Activity,
			NodeDocs:           repos.DocGen.LearningNodeDoc,
			DocRevisions:       repos.DocGen.LearningNodeDocRevision,
			DocVariants:        repos.DocGen.LearningNodeDocVariant,
			DocVariantExposure: repos.DocGen.DocVariantExposure,
			Chunks:             repos.Materials.MaterialChunk,
			MaterialSets:       repos.Materials.MaterialSet,
			MaterialFiles:      repos.Materials.MaterialFile,
			MaterialAssets:     repos.Materials.MaterialAsset,
			UserLibraryIndex:   repos.Library.UserLibraryIndex,
			Assets:             repos.Materials.Asset,
		},
		Learning: httpH.PathHandlerLearningRepos{
			Concepts:     repos.Concepts.Concept,
			Edges:        repos.Concepts.ConceptEdge,
			ConceptState: repos.Learning.UserConceptState,
			PolicyEval:   repos.Runtime.PolicyEvalSnapshot,
			PrereqGates:  repos.Learning.PrereqGateDecision,
		},
		Services: httpH.PathHandlerServices{
			Jobs:     repos.Jobs.JobRun,
			JobSvc:   services.JobService,
			Events:   services.Events,
			Avatar:   services.Avatar,
			Learning: learningUC,
			Bucket:   clients.GcpBucket,
		},
	})

	activityHandler := httpH.NewActivityHandlerWithDeps(httpH.ActivityHandlerDeps{
		Log: log,
		Repos: httpH.ActivityHandlerRepoDeps{
			Path:             repos.Paths.Path,
			PathNodes:        repos.Paths.PathNode,
			PathNodeActivity: repos.Paths.PathNodeActivity,
			Activities:       repos.Activities.Activity,
		},
	})

	eventHandler := httpH.NewEventHandlerWithDeps(httpH.EventHandlerDeps{
		Events: services.Events,
		Jobs:   services.JobService,
		Repos: httpH.EventHandlerRepoDeps{
			UserLibraryIndex: repos.Library.UserLibraryIndex,
			Path:             repos.Paths.Path,
		},
	})

	return Handlers{
		Health:   httpH.NewHealthHandler(),
		Auth:     httpH.NewAuthHandler(services.Auth),
		User:     httpH.NewUserHandler(services.User, sseHub, clients.GcpBucket),
		Session:  httpH.NewSessionStateHandler(services.SessionState),
		Runtime:  runtimeHandler,
		Realtime: httpH.NewRealtimeHandler(log, sseHub),
		Material: materialHandler,
		Chat:     httpH.NewChatHandler(services.Chat),
		Library:  libraryHandler,
		Path:     pathHandler,
		Activity: activityHandler,
		Event:    eventHandler,
		Gaze:     httpH.NewGazeHandler(services.Gaze),
		Job:      httpH.NewJobHandler(services.JobService),
	}
}

func wireRouter(log *logger.Logger, cfg Config, handlers Handlers, middleware Middleware, metrics *observability.Metrics) *gin.Engine {
	r := http.NewRouter(http.RouterConfig{
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
		RuntimeHandler:  handlers.Runtime,
		EventHandler:    handlers.Event,
		GazeHandler:     handlers.Gaze,
		JobHandler:      handlers.Job,
	})
	if log != nil {
		r.Use(httpMW.RequestLogger(log))
	}
	if metrics != nil {
		r.Use(httpMW.Metrics(metrics))
	}
	return r
}

func wireMiddleware(log *logger.Logger, services Services, cfg Config) Middleware {
	log.Info("Wiring middleware...")
	return Middleware{
		Auth: httpMW.NewAuthMiddleware(log, services.Auth),
	}
}
