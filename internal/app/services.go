package app

import (
	"fmt"
	"os"
	"strings"

	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/chain_signature_build"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/chat_maintain"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/chat_path_index"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/chat_path_node_index"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/chat_purge"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/chat_rebuild"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/chat_respond"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/chat_waitpoint_interpret"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/completed_unit_refresh"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/concept_bridge_build"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/concept_cluster_build"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/concept_graph_build"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/concept_graph_patch_build"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/coverage_coherence_audit"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/doc_probe_select"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/doc_variant_eval"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/embed_chunks"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/file_signature_build"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/ingest_chunks"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/learning_build"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/learning_build_progressive"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/library_taxonomy_refine"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/library_taxonomy_route"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/material_kg_build"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/material_set_summarize"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/material_signal_build"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/node_avatar_render"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/node_doc_build"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/node_doc_edit"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/node_doc_edit_apply"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/node_doc_patch"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/node_doc_prefetch"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/node_doc_progressive_build"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/node_figures_plan_build"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/node_figures_render"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/node_videos_plan_build"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/node_videos_render"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/path_cover_render"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/path_grouping_refine"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/path_intake"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/path_plan_build"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/path_structure_dispatch"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/path_structure_refine"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/policy_eval_refresh"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/policy_model_train"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/priors_refresh"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/progression_compact"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/psu_build"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/psu_promote"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/realize_activities"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/runtime_plan_build"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/runtime_update"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/saga_cleanup"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/structure_backfill"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/structure_extract"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/teaching_patterns_seed"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/user_model_update"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/user_profile_refresh"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/variant_stats_refresh"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/waitpoint_interpret"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/waitpoint_stage"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/web_resources_seed"
	jobruntime "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
	ingestion "github.com/yungbote/neurobridge-backend/internal/modules/learning/ingestion/pipeline"
	"github.com/yungbote/neurobridge-backend/internal/observability"
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
	// Gaze ingestion + aggregation
	Gaze services.GazeService

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

func wireServices(db *gorm.DB, log *logger.Logger, cfg Config, repos Repos, sseHub *realtime.SSEHub, clients Clients, metrics *observability.Metrics) (Services, error) {
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
	gazeService := services.NewGazeService(log, repos.UserGazeEvent, repos.UserGazeBlockStat, repos.UserPersonalizationPrefs)

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
	runtimeNotifier := services.NewRuntimeNotifier(emitter)

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
		repos.Path,
		repos.PathNode,
		repos.LearningNodeDoc,
		repos.Concept,
		repos.ConceptEdge,
		repos.UserConceptState,
		repos.UserConceptModel,
		repos.UserMisconception,
		repos.UserSessionState,
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
		repos.JobRun,
		jobService,
	)
	if err := jobRegistry.Register(chatMaintain); err != nil {
		return Services{}, err
	}

	structureExtract := structure_extract.New(
		db,
		log,
		clients.StructureExtractAI,
		repos.ChatThread,
		repos.ChatMessage,
		repos.ChatThreadState,
		repos.Concept,
		repos.UserConceptModel,
		repos.UserMisconception,
		repos.UserEvent,
		jobService,
	)
	if err := jobRegistry.Register(structureExtract); err != nil {
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

	chatPathNodeIndex := chat_path_node_index.New(
		db,
		log,
		clients.OpenaiClient,
		clients.PineconeVectorStore,
		repos.ChatDoc,
		repos.Path,
		repos.PathNode,
		repos.LearningNodeDoc,
	)
	if err := jobRegistry.Register(chatPathNodeIndex); err != nil {
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
		repos.UserPersonalizationPrefs,
		chatNotifier,
	)
	if err := jobRegistry.Register(waitpointInterpret); err != nil {
		return Services{}, err
	}

	chatWaitpointInterpret := chat_waitpoint_interpret.New(
		db,
		log,
		clients.OpenaiClient,
		repos.ChatThread,
		repos.ChatMessage,
		repos.ChatTurn,
		repos.JobRun,
		jobService,
		chatNotifier,
	)
	if err := jobRegistry.Register(chatWaitpointInterpret); err != nil {
		return Services{}, err
	}

	waitpointStage := waitpoint_stage.New(
		db,
		log,
		repos.ChatThread,
		repos.ChatMessage,
		chatNotifier,
	)
	if err := jobRegistry.Register(waitpointStage); err != nil {
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

	ingestChunks := ingest_chunks.New(db, log, repos.MaterialFile, repos.MaterialChunk, extractor, sagaSvc, bootstrapSvc, repos.LearningArtifact)
	if err := jobRegistry.Register(ingestChunks); err != nil {
		return Services{}, err
	}

	fileSignatureBuild := file_signature_build.New(
		db,
		log,
		repos.MaterialFile,
		repos.MaterialFileSignature,
		repos.MaterialFileSection,
		repos.MaterialChunk,
		clients.OpenaiClient,
		clients.PineconeVectorStore,
		sagaSvc,
		bootstrapSvc,
		repos.LearningArtifact,
	)
	if err := jobRegistry.Register(fileSignatureBuild); err != nil {
		return Services{}, err
	}

	embedChunks := embed_chunks.New(db, log, repos.MaterialFile, repos.MaterialChunk, clients.OpenaiClient, clients.PineconeVectorStore, sagaSvc, bootstrapSvc)
	if err := jobRegistry.Register(embedChunks); err != nil {
		return Services{}, err
	}

	materialSummarize := material_set_summarize.New(db, log, repos.MaterialFile, repos.MaterialChunk, repos.MaterialSetSummary, clients.OpenaiClient, clients.PineconeVectorStore, sagaSvc, bootstrapSvc, repos.LearningArtifact)
	if err := jobRegistry.Register(materialSummarize); err != nil {
		return Services{}, err
	}

	conceptGraph := concept_graph_build.New(db, log, repos.MaterialFile, repos.MaterialFileSignature, repos.MaterialChunk, repos.Path, repos.Concept, repos.ConceptRepresentation, repos.ConceptMappingOverride, repos.ConceptEvidence, repos.ConceptEdge, clients.Neo4j, clients.OpenaiClient, clients.PineconeVectorStore, sagaSvc, bootstrapSvc, repos.LearningArtifact)
	if err := jobRegistry.Register(conceptGraph); err != nil {
		return Services{}, err
	}

	conceptGraphPatch := concept_graph_patch_build.New(db, log, repos.MaterialFile, repos.MaterialFileSignature, repos.MaterialChunk, repos.Path, repos.Concept, repos.ConceptRepresentation, repos.ConceptMappingOverride, repos.ConceptEvidence, repos.ConceptEdge, clients.Neo4j, clients.OpenaiClient, clients.PineconeVectorStore, sagaSvc, bootstrapSvc, repos.LearningArtifact)
	if err := jobRegistry.Register(conceptGraphPatch); err != nil {
		return Services{}, err
	}

	conceptBridge := concept_bridge_build.New(db, log, repos.Concept, repos.ConceptEdge, clients.OpenaiClient, clients.PineconeVectorStore, bootstrapSvc)
	if err := jobRegistry.Register(conceptBridge); err != nil {
		return Services{}, err
	}

	materialSignal := material_signal_build.New(
		db,
		log,
		repos.MaterialFile,
		repos.MaterialFileSignature,
		repos.MaterialFileSection,
		repos.MaterialChunk,
		repos.Concept,
		repos.MaterialSet,
		clients.OpenaiClient,
		bootstrapSvc,
		repos.LearningArtifact,
	)
	if err := jobRegistry.Register(materialSignal); err != nil {
		return Services{}, err
	}

	materialKG := material_kg_build.New(db, log, repos.MaterialFile, repos.MaterialChunk, repos.Path, repos.Concept, clients.Neo4j, clients.OpenaiClient, sagaSvc, bootstrapSvc)
	if err := jobRegistry.Register(materialKG); err != nil {
		return Services{}, err
	}

	conceptCluster := concept_cluster_build.New(db, log, repos.Concept, repos.ConceptCluster, repos.ConceptClusterMember, clients.OpenaiClient, clients.PineconeVectorStore, sagaSvc, bootstrapSvc, repos.LearningArtifact)
	if err := jobRegistry.Register(conceptCluster); err != nil {
		return Services{}, err
	}

	chainSignatures := chain_signature_build.New(db, log, repos.Concept, repos.ConceptCluster, repos.ConceptClusterMember, repos.ConceptEdge, repos.ChainSignature, clients.OpenaiClient, clients.PineconeVectorStore, sagaSvc, bootstrapSvc, repos.LearningArtifact)
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
		repos.MaterialFileSignature,
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

	pathGroupingRefine := path_grouping_refine.New(
		db,
		log,
		repos.Path,
		repos.MaterialFile,
		repos.MaterialFileSignature,
		repos.UserPersonalizationPrefs,
		repos.ChatThread,
		repos.ChatMessage,
		chatNotifier,
	)
	if err := jobRegistry.Register(pathGroupingRefine); err != nil {
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
		repos.MaterialFile,
		repos.ChatThread,
		repos.ChatMessage,
		chatNotifier,
	)
	if err := jobRegistry.Register(pathStructureRefine); err != nil {
		return Services{}, err
	}

	pathPlan := path_plan_build.New(db, log, repos.Path, repos.PathNode, repos.Concept, repos.ConceptRepresentation, repos.ConceptEdge, repos.MaterialSetSummary, repos.UserProfileVector, repos.UserConceptState, repos.UserConceptModel, repos.UserMisconception, clients.Neo4j, clients.OpenaiClient, bootstrapSvc)
	if err := jobRegistry.Register(pathPlan); err != nil {
		return Services{}, err
	}

	runtimePlan := runtime_plan_build.New(
		db,
		log,
		repos.Path,
		repos.PathNode,
		repos.LearningNodeDoc,
		repos.MaterialSetSummary,
		repos.UserProfileVector,
		repos.UserProgressionEvent,
		clients.OpenaiClient,
		bootstrapSvc,
	)
	if err := jobRegistry.Register(runtimePlan); err != nil {
		return Services{}, err
	}

	psuBuild := psu_build.New(
		db,
		log,
		repos.PathNode,
		repos.Concept,
		repos.PathStructuralUnit,
		bootstrapSvc,
	)
	if err := jobRegistry.Register(psuBuild); err != nil {
		return Services{}, err
	}

	psuPromote := psu_promote.New(
		db,
		log,
		repos.UserEvent,
		repos.PathStructuralUnit,
		repos.Concept,
		repos.ConceptEdge,
		repos.UserConceptState,
		repos.UserConceptModel,
		repos.UserMisconception,
		clients.StructureExtractAI,
	)
	if err := jobRegistry.Register(psuPromote); err != nil {
		return Services{}, err
	}

	structureBackfill := structure_backfill.New(
		db,
		log,
		repos.Path,
		repos.PathNode,
		repos.Concept,
		repos.PathStructuralUnit,
		bootstrapSvc,
		repos.UserConceptState,
		repos.UserConceptModel,
	)
	if err := jobRegistry.Register(structureBackfill); err != nil {
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
		repos.LearningNodeDocBlueprint,
		repos.DocRetrievalPack,
		repos.DocGenerationTrace,
		repos.DocConstraintReport,
		repos.LearningNodeDocRevision,
		repos.MaterialFile,
		repos.MaterialChunk,
		repos.UserProfileVector,
		repos.TeachingPattern,
		repos.Concept,
		repos.UserConceptState,
		repos.UserConceptModel,
		repos.UserMisconception,
		clients.OpenaiClient,
		clients.PineconeVectorStore,
		clients.GcpBucket,
		bootstrapSvc,
	)
	if err := jobRegistry.Register(nodeDocs); err != nil {
		return Services{}, err
	}

	nodeDocPrefetch := node_doc_prefetch.New(
		db,
		log,
		repos.Path,
		repos.PathNode,
		repos.LearningNodeDoc,
		repos.LearningNodeFigure,
		repos.LearningNodeVideo,
		repos.DocGenerationRun,
		repos.LearningNodeDocBlueprint,
		repos.DocRetrievalPack,
		repos.DocGenerationTrace,
		repos.DocConstraintReport,
		repos.LearningNodeDocRevision,
		repos.MaterialFile,
		repos.MaterialChunk,
		repos.UserProfileVector,
		repos.TeachingPattern,
		repos.Concept,
		repos.UserConceptState,
		repos.UserConceptModel,
		repos.UserMisconception,
		clients.OpenaiClient,
		clients.PineconeVectorStore,
		clients.GcpBucket,
		bootstrapSvc,
	)
	if err := jobRegistry.Register(nodeDocPrefetch); err != nil {
		return Services{}, err
	}

	nodeDocProgressive := node_doc_progressive_build.New(
		db,
		log,
		repos.Path,
		repos.PathRun,
		repos.NodeRun,
		repos.PathNode,
		repos.LearningNodeDoc,
		repos.LearningNodeDocVariant,
		repos.UserDocSignalSnapshot,
		repos.InterventionPlan,
		repos.LearningNodeFigure,
		repos.LearningNodeVideo,
		repos.DocGenerationRun,
		repos.LearningNodeDocBlueprint,
		repos.DocRetrievalPack,
		repos.DocGenerationTrace,
		repos.DocConstraintReport,
		repos.LearningNodeDocRevision,
		repos.MaterialFile,
		repos.MaterialChunk,
		repos.UserProfileVector,
		repos.TeachingPattern,
		repos.Concept,
		repos.UserConceptState,
		repos.UserConceptModel,
		repos.UserMisconception,
		clients.OpenaiClient,
		clients.PineconeVectorStore,
		clients.GcpBucket,
		bootstrapSvc,
	)
	if err := jobRegistry.Register(nodeDocProgressive); err != nil {
		return Services{}, err
	}

	nodeDocPatch := node_doc_patch.New(
		db,
		log,
		jobService,
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

	nodeDocEdit := node_doc_edit.New(
		db,
		log,
		repos.ChatThread,
		repos.ChatMessage,
		repos.Path,
		repos.PathNode,
		repos.LearningNodeDoc,
		repos.LearningNodeFigure,
		repos.LearningNodeVideo,
		repos.MaterialFile,
		repos.MaterialChunk,
		repos.UserLibraryIndex,
		repos.Asset,
		clients.OpenaiClient,
		clients.PineconeVectorStore,
		clients.GcpBucket,
		chatNotifier,
	)
	if err := jobRegistry.Register(nodeDocEdit); err != nil {
		return Services{}, err
	}

	nodeDocEditApply := node_doc_edit_apply.New(
		db,
		log,
		repos.JobRun,
		jobService,
		repos.ChatThread,
		repos.PathNode,
		repos.LearningNodeDoc,
		repos.LearningNodeDocRevision,
	)
	if err := jobRegistry.Register(nodeDocEditApply); err != nil {
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
		repos.UserConceptModel,
		repos.UserMisconception,
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

	docProbeSelect := doc_probe_select.New(
		db,
		log,
		repos.Path,
		repos.PathRun,
		repos.PathNode,
		repos.LearningNodeDoc,
		repos.LearningNodeDocVariant,
		repos.Concept,
		repos.UserConceptState,
		repos.UserMisconception,
		repos.UserTestletState,
		repos.DocProbe,
		bootstrapSvc,
	)
	if err := jobRegistry.Register(docProbeSelect); err != nil {
		return Services{}, err
	}

	runtimeUpdate := runtime_update.New(
		db,
		log,
		repos.UserEvent,
		repos.UserEventCursor,
		repos.Path,
		repos.PathNode,
		repos.PathNodeActivity,
		repos.LearningNodeDoc,
		repos.PathRun,
		repos.NodeRun,
		repos.ActivityRun,
		repos.PathRunTransition,
		repos.UserSessionState,
		repos.Concept,
		repos.ConceptEdge,
		repos.UserConceptState,
		repos.UserConceptModel,
		repos.UserMisconception,
		repos.MisconceptionCausalEdge,
		repos.MisconceptionResolution,
		repos.UserBeliefSnapshot,
		repos.InterventionPlan,
		repos.ConceptReadinessSnapshot,
		repos.PrereqGateDecision,
		repos.UserTestletState,
		repos.DocProbe,
		repos.DocProbeOutcome,
		repos.DecisionTrace,
		repos.ModelSnapshot,
		repos.PolicyEvalSnapshot,
		jobService,
		runtimeNotifier,
		metrics,
	)
	if err := jobRegistry.Register(runtimeUpdate); err != nil {
		return Services{}, err
	}

	policyEval := policy_eval_refresh.New(db, log, repos.DecisionTrace, repos.PolicyEvalSnapshot)
	if err := jobRegistry.Register(policyEval); err != nil {
		return Services{}, err
	}

	policyTrain := policy_model_train.New(db, log, repos.DecisionTrace, repos.ModelSnapshot)
	if err := jobRegistry.Register(policyTrain); err != nil {
		return Services{}, err
	}

	variantStats := variant_stats_refresh.New(db, log, repos.UserEvent, repos.UserEventCursor, repos.ActivityVariant, repos.ActivityVariantStat, bootstrapSvc)
	if err := jobRegistry.Register(variantStats); err != nil {
		return Services{}, err
	}

	docVariantEval := doc_variant_eval.New(
		db,
		log,
		repos.DocVariantExposure,
		repos.DocVariantOutcome,
		repos.NodeRun,
		repos.UserConceptState,
		bootstrapSvc,
	)
	if err := jobRegistry.Register(docVariantEval); err != nil {
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

			Files:        repos.MaterialFile,
			FileSigs:     repos.MaterialFileSignature,
			FileSections: repos.MaterialFileSection,
			Chunks:       repos.MaterialChunk,
			MaterialSets: repos.MaterialSet,
			Summaries:    repos.MaterialSetSummary,

			Concepts:         repos.Concept,
			ConceptReps:      repos.ConceptRepresentation,
			MappingOverrides: repos.ConceptMappingOverride,
			Evidence:         repos.ConceptEvidence,
			Edges:            repos.ConceptEdge,

			Clusters: repos.ConceptCluster,
			Members:  repos.ConceptClusterMember,

			ChainSignatures:     repos.ChainSignature,
			PathStructuralUnits: repos.PathStructuralUnit,

			StylePrefs:       repos.UserStylePreference,
			ConceptState:     repos.UserConceptState,
			ConceptModel:     repos.UserConceptModel,
			MisconRepo:       repos.UserMisconception,
			ProgEvents:       repos.UserProgressionEvent,
			UserProfile:      repos.UserProfileVector,
			UserPrefs:        repos.UserPersonalizationPrefs,
			TeachingPatterns: repos.TeachingPattern,

			Path:                 repos.Path,
			PathNodes:            repos.PathNode,
			PathNodeActivities:   repos.PathNodeActivity,
			NodeDocs:             repos.LearningNodeDoc,
			NodeDocRevisions:     repos.LearningNodeDocRevision,
			NodeDocBlueprints:    repos.LearningNodeDocBlueprint,
			NodeFigures:          repos.LearningNodeFigure,
			NodeVideos:           repos.LearningNodeVideo,
			DocGenRuns:           repos.DocGenerationRun,
			DocRetrievalPacks:    repos.DocRetrievalPack,
			DocGenerationTraces:  repos.DocGenerationTrace,
			DocConstraintReports: repos.DocConstraintReport,
			Assets:               repos.Asset,
			Artifacts:            repos.LearningArtifact,

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
		metrics,
	)
	if err := jobRegistry.Register(learningBuild); err != nil {
		return Services{}, err
	}

	learningBuildProgressive := learning_build_progressive.New(
		db,
		log,
		jobService,
		repos.Path,
		repos.ChatThread,
		repos.ChatMessage,
		chatNotifier,
		sagaSvc,
		bootstrapSvc,
		&learning_build_progressive.InlineDeps{
			Extract: extractor,
			AI:      clients.OpenaiClient,
			Vec:     clients.PineconeVectorStore,
			Graph:   clients.Neo4j,
			Bucket:  clients.GcpBucket,
			Avatar:  avatarService,

			Files:        repos.MaterialFile,
			FileSigs:     repos.MaterialFileSignature,
			FileSections: repos.MaterialFileSection,
			Chunks:       repos.MaterialChunk,
			MaterialSets: repos.MaterialSet,
			Summaries:    repos.MaterialSetSummary,

			Concepts:         repos.Concept,
			ConceptReps:      repos.ConceptRepresentation,
			MappingOverrides: repos.ConceptMappingOverride,
			Evidence:         repos.ConceptEvidence,
			Edges:            repos.ConceptEdge,

			Clusters: repos.ConceptCluster,
			Members:  repos.ConceptClusterMember,

			ChainSignatures:     repos.ChainSignature,
			PathStructuralUnits: repos.PathStructuralUnit,

			StylePrefs:       repos.UserStylePreference,
			ConceptState:     repos.UserConceptState,
			ConceptModel:     repos.UserConceptModel,
			MisconRepo:       repos.UserMisconception,
			ProgEvents:       repos.UserProgressionEvent,
			UserProfile:      repos.UserProfileVector,
			UserPrefs:        repos.UserPersonalizationPrefs,
			TeachingPatterns: repos.TeachingPattern,

			Path:                 repos.Path,
			PathNodes:            repos.PathNode,
			PathNodeActivities:   repos.PathNodeActivity,
			NodeDocs:             repos.LearningNodeDoc,
			NodeDocRevisions:     repos.LearningNodeDocRevision,
			NodeDocBlueprints:    repos.LearningNodeDocBlueprint,
			NodeFigures:          repos.LearningNodeFigure,
			NodeVideos:           repos.LearningNodeVideo,
			DocGenRuns:           repos.DocGenerationRun,
			DocRetrievalPacks:    repos.DocRetrievalPack,
			DocGenerationTraces:  repos.DocGenerationTrace,
			DocConstraintReports: repos.DocConstraintReport,
			Assets:               repos.Asset,
			Artifacts:            repos.LearningArtifact,

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
		metrics,
	)
	if err := jobRegistry.Register(learningBuildProgressive); err != nil {
		return Services{}, err
	}

	userModel := user_model_update.New(
		db,
		log,
		repos.UserEvent,
		repos.UserEventCursor,
		repos.Concept,
		repos.ConceptEdge,
		repos.ActivityConcept,
		repos.UserConceptState,
		repos.UserConceptModel,
		repos.UserConceptEdgeStat,
		repos.UserConceptEvidence,
		repos.UserConceptCalibration,
		repos.ItemCalibration,
		repos.UserModelAlert,
		repos.UserMisconception,
		repos.MisconceptionCausalEdge,
		repos.UserStylePreference,
		repos.ConceptClusterMember,
		repos.UserTestletState,
		repos.UserSkillState,
		clients.Neo4j,
		repos.JobRun,
		jobService,
	)
	if err := jobRegistry.Register(userModel); err != nil {
		return Services{}, err
	}

	var temporalRunner *temporalworker.Runner
	if runWorker {
		w, err := temporalworker.NewRunner(log, clients.Temporal, db, repos.JobRun, jobRegistry, jobNotifier, metrics)
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
		repos.UserSessionState,
	)

	return Services{
		Avatar:           avatarService,
		File:             fileService,
		Auth:             authService,
		User:             userService,
		Material:         materialService,
		Events:           eventService,
		SessionState:     sessionStateService,
		Gaze:             gazeService,
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
