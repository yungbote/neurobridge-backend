package app

import (
	"fmt"
	"os"
	"strings"

	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/chain_signature_build"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/chat_maintain"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/chat_path_index"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/chat_purge"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/chat_rebuild"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/chat_respond"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/completed_unit_refresh"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/concept_cluster_build"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/concept_graph_build"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/coverage_coherence_audit"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/embed_chunks"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/ingest_chunks"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/learning_build"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/library_taxonomy_refine"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/library_taxonomy_route"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/material_kg_build"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/material_set_summarize"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/node_avatar_render"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/node_doc_build"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/node_doc_patch"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/node_figures_plan_build"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/node_figures_render"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/node_videos_plan_build"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/node_videos_render"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/path_cover_render"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/path_intake"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/path_plan_build"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/path_structure_dispatch"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/path_structure_refine"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/priors_refresh"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/progression_compact"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/realize_activities"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/saga_cleanup"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/teaching_patterns_seed"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/user_model_update"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/user_profile_refresh"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/variant_stats_refresh"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/waitpoint_interpret"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/web_resources_seed"
	jobruntime "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
	ingestion "github.com/yungbote/neurobridge-backend/internal/modules/learning/ingestion/pipeline"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/realtime"
	"github.com/yungbote/neurobridge-backend/internal/realtime/bus"
	"github.com/yungbote/neurobridge-backend/internal/services"
	"github.com/yungbote/neurobridge-backend/internal/temporalx"
	"github.com/yungbote/neurobridge-backend/internal/temporalx/temporalworker"
)

type Services struct {
	// Core
	Avatar services.AvatarService
	File   services.FileService

	// Auth + domain
	Auth    services.AuthService
	OIDCVer services.OIDCVerifier

	User     services.UserService
	Material services.MaterialService

	// User Event Ingestion (raw user_event log)
	Events services.EventService
	// Runtime per-session state (active path/node/etc)
	SessionState services.SessionStateService

	// Jobs + notifications
	JobNotifier  services.JobNotifier
	JobService   services.JobService
	Workflow     services.WorkflowService
	ChatNotifier services.ChatNotifier
	Chat         services.ChatService

	// Orchestrator
	ContentExtractor ingestion.ContentExtractionService

	// Job infra
	JobRegistry    *jobruntime.Registry
	TemporalWorker *temporalworker.Runner

	// Keep bus here for convenience/compat
	SSEBus bus.Bus
}

func wireServices(db *gorm.DB, log *logger.Logger, cfg Config, repos Repos, sseHub *realtime.SSEHub, clients Clients) (Services, error) {
	log.Info("Wiring services...")

	avatarService, err := services.NewAvatarService(db, log, repos.User, clients.GcpBucket, clients.OpenaiClient)
	if err != nil {
		return Services{}, fmt.Errorf("init avatar service: %w", err)
	}

	fileService := services.NewFileService(db, log, clients.GcpBucket, repos.MaterialFile)

	oidcVerifier, err := services.NewOIDCVerifier(nil, cfg.GoogleOIDCClientID, cfg.AppleOIDCClientID)
	if err != nil {
		panic(err)
	}

	authService := services.NewAuthService(
		db, log,
		repos.User,
		avatarService,
		repos.UserToken,
		repos.UserIdentity,
		repos.OAuthNonce,
		oidcVerifier,
		cfg.JWTSecretKey,
		cfg.AccessTokenTTL,
		cfg.RefreshTokenTTL,
		cfg.NonceRefreshTTL,
	)

	userService := services.NewUserService(db, log, repos.User, repos.UserPersonalizationPrefs, avatarService)
	materialService := services.NewMaterialService(db, log, repos.MaterialSet, repos.MaterialFile, fileService)
	eventService := services.NewEventService(db, log, repos.UserEvent)
	sessionStateService := services.NewSessionStateService(db, log, repos.UserSessionState)

	runServer := strings.EqualFold(strings.TrimSpace(os.Getenv("RUN_SERVER")), "true")
	runWorker := strings.EqualFold(strings.TrimSpace(os.Getenv("RUN_WORKER")), "true")

	var emitter services.SSEEmitter
	if runServer {
		// API: broadcast locally to connected clients
		emitter = &services.HubEmitter{Hub: sseHub}
	} else {
		// Worker: publish to Redis so API can fan-out to clients
		if clients.SSEBus == nil {
			return Services{}, fmt.Errorf("worker requires REDIS_ADDR to publish SSE events")
		}
		emitter = &services.RedisEmitter{Bus: clients.SSEBus}
	}

	jobNotifier := services.NewJobNotifier(emitter)
	tc := clients.Temporal
	tcfg := temporalx.LoadConfig()
	jobService := services.NewJobService(db, log, repos.JobRun, jobNotifier, tc, tcfg.TaskQueue)

	// Shared bootstrap service (used by workflows + learning pipelines).
	bootstrapSvc := services.NewLearningBuildBootstrapService(db, log, repos.Path, repos.UserLibraryIndex)

	workflow := services.NewWorkflowService(db, log, materialService, jobService, bootstrapSvc, repos.Path, repos.ChatThread, repos.ChatMessage)
	chatNotifier := services.NewChatNotifier(emitter)

	extractor := ingestion.NewContentExtractionService(
		db,
		log,
		repos.MaterialChunk,
		repos.MaterialFile,
		repos.MaterialAsset,
		clients.GcpBucket,
		clients.LMTools,
		clients.GcpDocument,
		clients.GcpVision,
		clients.GcpSpeech,
		clients.GcpVideo,
		clients.OpenaiCaption,
	)

	// Job registry
	jobRegistry := jobruntime.NewRegistry()

	// --------------------
	// Chat pipelines
	// --------------------
	chatRespond := chat_respond.New(
		db,
		log,
		clients.OpenaiClient,
		clients.PineconeVectorStore,
		repos.ChatThread,
		repos.ChatMessage,
		repos.ChatThreadState,
		repos.ChatSummaryNode,
		repos.ChatDoc,
		repos.ChatTurn,
		repos.JobRun,
		jobService,
		chatNotifier,
	)
	if err := jobRegistry.Register(chatRespond); err != nil {
		return Services{}, err
	}

	chatMaintain := chat_maintain.New(
		db,
		log,
		clients.OpenaiClient,
		clients.PineconeVectorStore,
		clients.Neo4j,
		repos.ChatThread,
		repos.ChatMessage,
		repos.ChatThreadState,
		repos.ChatSummaryNode,
		repos.ChatDoc,
		repos.ChatMemoryItem,
		repos.ChatEntity,
		repos.ChatEdge,
		repos.ChatClaim,
	)
	if err := jobRegistry.Register(chatMaintain); err != nil {
		return Services{}, err
	}

	chatRebuild := chat_rebuild.New(
		db,
		log,
		clients.PineconeVectorStore,
		repos.JobRun,
		jobService,
	)
	if err := jobRegistry.Register(chatRebuild); err != nil {
		return Services{}, err
	}

	chatPurge := chat_purge.New(
		db,
		log,
		clients.PineconeVectorStore,
	)
	if err := jobRegistry.Register(chatPurge); err != nil {
		return Services{}, err
	}

	chatPathIndex := chat_path_index.New(
		db,
		log,
		clients.OpenaiClient,
		clients.PineconeVectorStore,
		repos.ChatDoc,
		repos.Path,
		repos.PathNode,
		repos.PathNodeActivity,
		repos.Activity,
		repos.Concept,
		repos.LearningNodeDoc,
		repos.UserLibraryIndex,
		repos.MaterialFile,
		repos.MaterialSetSummary,
	)
	if err := jobRegistry.Register(chatPathIndex); err != nil {
		return Services{}, err
	}

	waitpointInterpret := waitpoint_interpret.New(
		db,
		log,
		clients.OpenaiClient,
		repos.ChatThread,
		repos.ChatMessage,
		repos.ChatTurn,
		repos.JobRun,
		jobService,
		repos.Path,
		chatNotifier,
	)
	if err := jobRegistry.Register(waitpointInterpret); err != nil {
		return Services{}, err
	}

	// Shared learning_build services (durable saga + path bootstrap).
	sagaSvc := services.NewSagaService(db, log, repos.SagaRun, repos.SagaAction, clients.GcpBucket, clients.PineconeVectorStore)

	// --------------------
	// Learning build (Path-centric) pipelines
	// --------------------
	webResourcesSeed := web_resources_seed.New(
		db,
		log,
		repos.MaterialFile,
		repos.Path,
		clients.GcpBucket,
		repos.ChatThread,
		repos.ChatMessage,
		chatNotifier,
		clients.OpenaiClient,
		sagaSvc,
		bootstrapSvc,
	)
	if err := jobRegistry.Register(webResourcesSeed); err != nil {
		return Services{}, err
	}

	ingestChunks := ingest_chunks.New(db, log, repos.MaterialFile, repos.MaterialChunk, extractor, sagaSvc, bootstrapSvc)
	if err := jobRegistry.Register(ingestChunks); err != nil {
		return Services{}, err
	}

	embedChunks := embed_chunks.New(db, log, repos.MaterialFile, repos.MaterialChunk, clients.OpenaiClient, clients.PineconeVectorStore, sagaSvc, bootstrapSvc)
	if err := jobRegistry.Register(embedChunks); err != nil {
		return Services{}, err
	}

	materialSummarize := material_set_summarize.New(db, log, repos.MaterialFile, repos.MaterialChunk, repos.MaterialSetSummary, clients.OpenaiClient, clients.PineconeVectorStore, sagaSvc, bootstrapSvc)
	if err := jobRegistry.Register(materialSummarize); err != nil {
		return Services{}, err
	}

	conceptGraph := concept_graph_build.New(db, log, repos.MaterialFile, repos.MaterialChunk, repos.Path, repos.Concept, repos.ConceptEvidence, repos.ConceptEdge, clients.Neo4j, clients.OpenaiClient, clients.PineconeVectorStore, sagaSvc, bootstrapSvc)
	if err := jobRegistry.Register(conceptGraph); err != nil {
		return Services{}, err
	}

	materialKG := material_kg_build.New(db, log, repos.MaterialFile, repos.MaterialChunk, repos.Path, repos.Concept, clients.Neo4j, clients.OpenaiClient, sagaSvc, bootstrapSvc)
	if err := jobRegistry.Register(materialKG); err != nil {
		return Services{}, err
	}

	conceptCluster := concept_cluster_build.New(db, log, repos.Concept, repos.ConceptCluster, repos.ConceptClusterMember, clients.OpenaiClient, clients.PineconeVectorStore, sagaSvc, bootstrapSvc)
	if err := jobRegistry.Register(conceptCluster); err != nil {
		return Services{}, err
	}

	chainSignatures := chain_signature_build.New(db, log, repos.Concept, repos.ConceptCluster, repos.ConceptClusterMember, repos.ConceptEdge, repos.ChainSignature, clients.OpenaiClient, clients.PineconeVectorStore, sagaSvc, bootstrapSvc)
	if err := jobRegistry.Register(chainSignatures); err != nil {
		return Services{}, err
	}

	userProfileRefresh := user_profile_refresh.New(db, log, repos.UserStylePreference, repos.UserProgressionEvent, repos.UserProfileVector, repos.UserPersonalizationPrefs, clients.OpenaiClient, clients.PineconeVectorStore, sagaSvc, bootstrapSvc)
	if err := jobRegistry.Register(userProfileRefresh); err != nil {
		return Services{}, err
	}

	teachingPatterns := teaching_patterns_seed.New(db, log, repos.TeachingPattern, repos.UserProfileVector, clients.OpenaiClient, clients.PineconeVectorStore, sagaSvc, bootstrapSvc)
	if err := jobRegistry.Register(teachingPatterns); err != nil {
		return Services{}, err
	}

	pathIntake := path_intake.New(
		db,
		log,
		repos.MaterialFile,
		repos.MaterialChunk,
		repos.MaterialSetSummary,
		repos.Path,
		repos.UserPersonalizationPrefs,
		repos.ChatThread,
		repos.ChatMessage,
		clients.OpenaiClient,
		chatNotifier,
		bootstrapSvc,
	)
	if err := jobRegistry.Register(pathIntake); err != nil {
		return Services{}, err
	}

	pathStructureDispatch := path_structure_dispatch.New(
		db,
		log,
		jobService,
		repos.JobRun,
		repos.Path,
		repos.MaterialFile,
		repos.MaterialSet,
		repos.MaterialSetFile,
		repos.UserLibraryIndex,
	)
	if err := jobRegistry.Register(pathStructureDispatch); err != nil {
		return Services{}, err
	}

	pathStructureRefine := path_structure_refine.New(
		db,
		log,
		repos.Path,
		repos.Concept,
		repos.ChatThread,
		repos.ChatMessage,
		chatNotifier,
	)
	if err := jobRegistry.Register(pathStructureRefine); err != nil {
		return Services{}, err
	}

	pathPlan := path_plan_build.New(db, log, repos.Path, repos.PathNode, repos.Concept, repos.ConceptEdge, repos.MaterialSetSummary, repos.UserProfileVector, repos.UserConceptState, clients.Neo4j, clients.OpenaiClient, bootstrapSvc)
	if err := jobRegistry.Register(pathPlan); err != nil {
		return Services{}, err
	}

	libraryTaxonomyRoute := library_taxonomy_route.New(
		db,
		log,
		clients.OpenaiClient,
		clients.Neo4j,
		jobService,
		repos.JobRun,
		repos.Path,
		repos.PathNode,
		repos.ConceptCluster,
		repos.LibraryTaxonomyNode,
		repos.LibraryTaxonomyEdge,
		repos.LibraryTaxonomyMember,
		repos.LibraryTaxonomyState,
		repos.LibraryTaxonomySnapshot,
		repos.LibraryPathEmbedding,
	)
	if err := jobRegistry.Register(libraryTaxonomyRoute); err != nil {
		return Services{}, err
	}

	libraryTaxonomyRefine := library_taxonomy_refine.New(
		db,
		log,
		clients.OpenaiClient,
		clients.Neo4j,
		repos.Path,
		repos.PathNode,
		repos.ConceptCluster,
		repos.LibraryTaxonomyNode,
		repos.LibraryTaxonomyEdge,
		repos.LibraryTaxonomyMember,
		repos.LibraryTaxonomyState,
		repos.LibraryTaxonomySnapshot,
		repos.LibraryPathEmbedding,
	)
	if err := jobRegistry.Register(libraryTaxonomyRefine); err != nil {
		return Services{}, err
	}

	pathCoverRender := path_cover_render.New(
		db,
		log,
		repos.Path,
		repos.PathNode,
		avatarService,
		bootstrapSvc,
	)
	if err := jobRegistry.Register(pathCoverRender); err != nil {
		return Services{}, err
	}

	nodeAvatarRender := node_avatar_render.New(
		db,
		log,
		repos.Path,
		repos.PathNode,
		avatarService,
		bootstrapSvc,
	)
	if err := jobRegistry.Register(nodeAvatarRender); err != nil {
		return Services{}, err
	}

	nodeFiguresPlan := node_figures_plan_build.New(
		db,
		log,
		repos.Path,
		repos.PathNode,
		repos.LearningNodeFigure,
		repos.DocGenerationRun,
		repos.MaterialFile,
		repos.MaterialChunk,
		clients.OpenaiClient,
		clients.PineconeVectorStore,
		bootstrapSvc,
	)
	if err := jobRegistry.Register(nodeFiguresPlan); err != nil {
		return Services{}, err
	}

	nodeFiguresRender := node_figures_render.New(
		db,
		log,
		repos.Path,
		repos.PathNode,
		repos.LearningNodeFigure,
		repos.Asset,
		repos.DocGenerationRun,
		clients.OpenaiClient,
		clients.GcpBucket,
		bootstrapSvc,
	)
	if err := jobRegistry.Register(nodeFiguresRender); err != nil {
		return Services{}, err
	}

	nodeVideosPlan := node_videos_plan_build.New(
		db,
		log,
		repos.Path,
		repos.PathNode,
		repos.LearningNodeVideo,
		repos.DocGenerationRun,
		repos.MaterialFile,
		repos.MaterialChunk,
		clients.OpenaiClient,
		clients.PineconeVectorStore,
		bootstrapSvc,
	)
	if err := jobRegistry.Register(nodeVideosPlan); err != nil {
		return Services{}, err
	}

	nodeVideosRender := node_videos_render.New(
		db,
		log,
		repos.Path,
		repos.PathNode,
		repos.LearningNodeVideo,
		repos.Asset,
		repos.DocGenerationRun,
		clients.OpenaiClient,
		clients.GcpBucket,
		bootstrapSvc,
	)
	if err := jobRegistry.Register(nodeVideosRender); err != nil {
		return Services{}, err
	}

	nodeDocs := node_doc_build.New(
		db,
		log,
		repos.Path,
		repos.PathNode,
		repos.LearningNodeDoc,
		repos.LearningNodeFigure,
		repos.LearningNodeVideo,
		repos.DocGenerationRun,
		repos.MaterialFile,
		repos.MaterialChunk,
		repos.UserProfileVector,
		repos.TeachingPattern,
		repos.Concept,
		repos.UserConceptState,
		clients.OpenaiClient,
		clients.PineconeVectorStore,
		clients.GcpBucket,
		bootstrapSvc,
	)
	if err := jobRegistry.Register(nodeDocs); err != nil {
		return Services{}, err
	}

	nodeDocPatch := node_doc_patch.New(
		db,
		log,
		repos.Path,
		repos.PathNode,
		repos.LearningNodeDoc,
		repos.LearningNodeFigure,
		repos.LearningNodeVideo,
		repos.LearningNodeDocRevision,
		repos.MaterialFile,
		repos.MaterialChunk,
		repos.UserLibraryIndex,
		repos.Asset,
		clients.OpenaiClient,
		clients.PineconeVectorStore,
		clients.GcpBucket,
	)
	if err := jobRegistry.Register(nodeDocPatch); err != nil {
		return Services{}, err
	}

	realizeActivities := realize_activities.New(
		db,
		log,
		repos.Path,
		repos.PathNode,
		repos.PathNodeActivity,
		repos.Activity,
		repos.ActivityVariant,
		repos.ActivityConcept,
		repos.ActivityCitation,
		repos.Concept,
		repos.UserConceptState,
		repos.MaterialFile,
		repos.MaterialChunk,
		repos.UserProfileVector,
		repos.TeachingPattern,
		clients.Neo4j,
		clients.OpenaiClient,
		clients.PineconeVectorStore,
		clients.GcpBucket,
		sagaSvc,
		bootstrapSvc,
	)
	if err := jobRegistry.Register(realizeActivities); err != nil {
		return Services{}, err
	}

	coverageAudit := coverage_coherence_audit.New(db, log, repos.Path, repos.PathNode, repos.Concept, repos.Activity, repos.ActivityVariant, clients.OpenaiClient, bootstrapSvc)
	if err := jobRegistry.Register(coverageAudit); err != nil {
		return Services{}, err
	}

	progressionCompact := progression_compact.New(db, log, repos.UserEvent, repos.UserEventCursor, repos.UserProgressionEvent, bootstrapSvc)
	if err := jobRegistry.Register(progressionCompact); err != nil {
		return Services{}, err
	}

	variantStats := variant_stats_refresh.New(db, log, repos.UserEvent, repos.UserEventCursor, repos.ActivityVariant, repos.ActivityVariantStat, bootstrapSvc)
	if err := jobRegistry.Register(variantStats); err != nil {
		return Services{}, err
	}

	priors := priors_refresh.New(
		db,
		log,
		repos.Activity,
		repos.ActivityVariant,
		repos.ActivityVariantStat,
		repos.ChainSignature,
		repos.Concept,
		repos.ActivityConcept,
		repos.ChainPrior,
		repos.CohortPrior,
		bootstrapSvc,
	)
	if err := jobRegistry.Register(priors); err != nil {
		return Services{}, err
	}

	completedUnits := completed_unit_refresh.New(
		db,
		log,
		repos.UserCompletedUnit,
		repos.UserProgressionEvent,
		repos.Concept,
		repos.Activity,
		repos.ActivityConcept,
		repos.ChainSignature,
		repos.UserConceptState,
		clients.Neo4j,
		bootstrapSvc,
	)
	if err := jobRegistry.Register(completedUnits); err != nil {
		return Services{}, err
	}

	sagaCleanup := saga_cleanup.New(db, log, repos.SagaRun, sagaSvc, clients.GcpBucket)
	if err := jobRegistry.Register(sagaCleanup); err != nil {
		return Services{}, err
	}

	learningBuild := learning_build.New(
		db,
		log,
		jobService,
		repos.Path,
		repos.ChatThread,
		repos.ChatMessage,
		chatNotifier,
		sagaSvc,
		bootstrapSvc,
		&learning_build.InlineDeps{
			Extract: extractor,
			AI:      clients.OpenaiClient,
			Vec:     clients.PineconeVectorStore,
			Graph:   clients.Neo4j,
			Bucket:  clients.GcpBucket,
			Avatar:  avatarService,

			Files:     repos.MaterialFile,
			Chunks:    repos.MaterialChunk,
			Summaries: repos.MaterialSetSummary,

			Concepts: repos.Concept,
			Evidence: repos.ConceptEvidence,
			Edges:    repos.ConceptEdge,

			Clusters: repos.ConceptCluster,
			Members:  repos.ConceptClusterMember,

			ChainSignatures: repos.ChainSignature,

			StylePrefs:       repos.UserStylePreference,
			ConceptState:     repos.UserConceptState,
			ProgEvents:       repos.UserProgressionEvent,
			UserProfile:      repos.UserProfileVector,
			UserPrefs:        repos.UserPersonalizationPrefs,
			TeachingPatterns: repos.TeachingPattern,

			Path:               repos.Path,
			PathNodes:          repos.PathNode,
			PathNodeActivities: repos.PathNodeActivity,
			NodeDocs:           repos.LearningNodeDoc,
			NodeFigures:        repos.LearningNodeFigure,
			NodeVideos:         repos.LearningNodeVideo,
			DocGenRuns:         repos.DocGenerationRun,
			Assets:             repos.Asset,

			Activities:        repos.Activity,
			Variants:          repos.ActivityVariant,
			ActivityConcepts:  repos.ActivityConcept,
			ActivityCitations: repos.ActivityCitation,

			UserEvents:            repos.UserEvent,
			UserEventCursors:      repos.UserEventCursor,
			UserProgressionEvents: repos.UserProgressionEvent,
			VariantStats:          repos.ActivityVariantStat,

			ChainPriors:    repos.ChainPrior,
			CohortPriors:   repos.CohortPrior,
			CompletedUnits: repos.UserCompletedUnit,
		},
	)
	if err := jobRegistry.Register(learningBuild); err != nil {
		return Services{}, err
	}

	userModel := user_model_update.New(
		db,
		log,
		repos.UserEvent,
		repos.UserEventCursor,
		repos.Concept,
		repos.ActivityConcept,
		repos.UserConceptState,
		repos.UserStylePreference,
		clients.Neo4j,
		repos.JobRun,
	)
	if err := jobRegistry.Register(userModel); err != nil {
		return Services{}, err
	}

	var temporalRunner *temporalworker.Runner
	if runWorker {
		w, err := temporalworker.NewRunner(log, clients.Temporal, db, repos.JobRun, jobRegistry, jobNotifier)
		if err != nil {
			return Services{}, fmt.Errorf("init temporal worker: %w", err)
		}
		temporalRunner = w
	}

	chatService := services.NewChatService(
		db,
		log,
		repos.Path,
		repos.JobRun,
		jobService,
		repos.ChatThread,
		repos.ChatMessage,
		repos.ChatTurn,
		chatNotifier,
	)

	return Services{
		Avatar:           avatarService,
		File:             fileService,
		Auth:             authService,
		User:             userService,
		Material:         materialService,
		Events:           eventService,
		SessionState:     sessionStateService,
		JobNotifier:      jobNotifier,
		JobService:       jobService,
		Workflow:         workflow,
		ChatNotifier:     chatNotifier,
		Chat:             chatService,
		ContentExtractor: extractor,
		JobRegistry:      jobRegistry,
		TemporalWorker:   temporalRunner,
		SSEBus:           clients.SSEBus,
	}, nil
}
