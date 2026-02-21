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
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/graph_version_rollback"
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
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/structural_drift_monitor"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/structural_trace_backfill"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/structure_backfill"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/structure_extract"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/teaching_patterns_seed"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/trace_compact"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/trace_load_test"
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

	avatarService, err := services.NewAvatarService(db, log, repos.Auth.User, clients.GcpBucket, clients.OpenaiClient)
	if err != nil {
		return Services{}, fmt.Errorf("init avatar service: %w", err)
	}

	fileService := services.NewFileService(db, log, clients.GcpBucket, repos.Materials.MaterialFile)

	oidcVerifier, err := services.NewOIDCVerifier(nil, cfg.GoogleOIDCClientID, cfg.AppleOIDCClientID)
	if err != nil {
		panic(err)
	}

	authService := services.NewAuthService(
		db, log,
		repos.Auth.User,
		avatarService,
		repos.Auth.UserToken,
		repos.Auth.UserIdentity,
		repos.Auth.OAuthNonce,
		oidcVerifier,
		cfg.JWTSecretKey,
		cfg.AccessTokenTTL,
		cfg.RefreshTokenTTL,
		cfg.NonceRefreshTTL,
	)

	userService := services.NewUserService(db, log, repos.Auth.User, repos.Users.UserPersonalizationPrefs, avatarService)
	materialService := services.NewMaterialService(db, log, repos.Materials.MaterialSet, repos.Materials.MaterialFile, fileService)
	eventService := services.NewEventService(db, log, repos.Events.UserEvent)
	sessionStateService := services.NewSessionStateService(db, log, repos.Users.UserSessionState)
	gazeService := services.NewGazeService(log, repos.Users.UserGazeEvent, repos.Users.UserGazeBlockStat, repos.Users.UserPersonalizationPrefs)

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
	jobService := services.NewJobService(db, log, repos.Jobs.JobRun, jobNotifier, tc, tcfg.TaskQueue)

	// Shared bootstrap service (used by workflows + learning pipelines).
	bootstrapSvc := services.NewLearningBuildBootstrapService(db, log, repos.Paths.Path, repos.Library.UserLibraryIndex)

	workflow := services.NewWorkflowService(db, log, materialService, jobService, bootstrapSvc, repos.Paths.Path, repos.Chat.ChatThread, repos.Chat.ChatMessage)
	chatNotifier := services.NewChatNotifier(emitter)
	runtimeNotifier := services.NewRuntimeNotifier(emitter)

	extractor := ingestion.NewContentExtractionService(
		db,
		log,
		repos.Materials.MaterialChunk,
		repos.Materials.MaterialFile,
		repos.Materials.MaterialAsset,
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
		repos.Chat.ChatThread,
		repos.Chat.ChatMessage,
		repos.Chat.ChatThreadState,
		repos.Chat.ChatSummaryNode,
		repos.Chat.Thread,
		repos.Chat.ChatDoc,
		repos.Chat.ChatTurn,
		repos.Paths.Path,
		repos.Paths.PathNode,
		repos.DocGen.LearningNodeDoc,
		repos.Concepts.Concept,
		repos.Concepts.ConceptEdge,
		repos.Learning.UserConceptState,
		repos.Learning.UserConceptModel,
		repos.Learning.UserMisconception,
		repos.Users.UserSessionState,
		repos.Jobs.JobRun,
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
		repos.Chat.ChatThread,
		repos.Chat.ChatMessage,
		repos.Chat.ChatThreadState,
		repos.Chat.ChatSummaryNode,
		repos.Chat.ChatDoc,
		repos.Chat.ChatMemoryItem,
		repos.Chat.ChatEntity,
		repos.Chat.ChatEdge,
		repos.Chat.ChatClaim,
		repos.Jobs.JobRun,
		jobService,
	)
	if err := jobRegistry.Register(chatMaintain); err != nil {
		return Services{}, err
	}

	structureExtract := structure_extract.New(
		db,
		log,
		clients.StructureExtractAI,
		repos.Chat.ChatThread,
		repos.Chat.ChatMessage,
		repos.Chat.ChatTurn,
		repos.Chat.ChatThreadState,
		repos.Concepts.Concept,
		repos.Learning.UserConceptModel,
		repos.Learning.UserMisconception,
		repos.Events.UserEvent,
		jobService,
	)
	if err := jobRegistry.Register(structureExtract); err != nil {
		return Services{}, err
	}

	chatRebuild := chat_rebuild.New(
		db,
		log,
		clients.PineconeVectorStore,
		repos.Jobs.JobRun,
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
		repos.Chat.ChatDoc,
		repos.Paths.Path,
		repos.Paths.PathNode,
		repos.Paths.PathNodeActivity,
		repos.Activities.Activity,
		repos.Concepts.Concept,
		repos.DocGen.LearningNodeDoc,
		repos.Library.UserLibraryIndex,
		repos.Materials.MaterialFile,
		repos.Materials.MaterialSetSummary,
	)
	if err := jobRegistry.Register(chatPathIndex); err != nil {
		return Services{}, err
	}

	chatPathNodeIndex := chat_path_node_index.New(
		db,
		log,
		clients.OpenaiClient,
		clients.PineconeVectorStore,
		repos.Chat.ChatDoc,
		repos.Paths.Path,
		repos.Paths.PathNode,
		repos.DocGen.LearningNodeDoc,
	)
	if err := jobRegistry.Register(chatPathNodeIndex); err != nil {
		return Services{}, err
	}

	waitpointInterpret := waitpoint_interpret.New(
		db,
		log,
		clients.OpenaiClient,
		repos.Chat.ChatThread,
		repos.Chat.ChatMessage,
		repos.Chat.ChatTurn,
		repos.Jobs.JobRun,
		jobService,
		repos.Paths.Path,
		repos.Users.UserPersonalizationPrefs,
		repos.Runtime.DecisionTrace,
		chatNotifier,
	)
	if err := jobRegistry.Register(waitpointInterpret); err != nil {
		return Services{}, err
	}

	chatWaitpointInterpret := chat_waitpoint_interpret.New(
		db,
		log,
		clients.OpenaiClient,
		repos.Chat.ChatThread,
		repos.Chat.ChatMessage,
		repos.Chat.ChatTurn,
		repos.Jobs.JobRun,
		jobService,
		repos.Runtime.DecisionTrace,
		chatNotifier,
	)
	if err := jobRegistry.Register(chatWaitpointInterpret); err != nil {
		return Services{}, err
	}

	waitpointStage := waitpoint_stage.New(
		db,
		log,
		repos.Chat.ChatThread,
		repos.Chat.ChatMessage,
		chatNotifier,
	)
	if err := jobRegistry.Register(waitpointStage); err != nil {
		return Services{}, err
	}

	// Shared learning_build services (durable saga + path bootstrap).
	sagaSvc := services.NewSagaService(
		db,
		log,
		repos.Jobs.SagaRun,
		repos.Jobs.SagaAction,
		repos.Jobs.Saga,
		clients.GcpBucket,
		clients.PineconeVectorStore,
		cfg.VectorProvider,
	)

	// --------------------
	// Learning build (Path-centric) pipelines
	// --------------------
	webResourcesSeed := web_resources_seed.New(
		db,
		log,
		repos.Materials.MaterialFile,
		repos.Paths.Path,
		clients.GcpBucket,
		repos.Chat.ChatThread,
		repos.Chat.ChatMessage,
		chatNotifier,
		clients.OpenaiClient,
		sagaSvc,
		bootstrapSvc,
	)
	if err := jobRegistry.Register(webResourcesSeed); err != nil {
		return Services{}, err
	}

	ingestChunks := ingest_chunks.New(db, log, repos.Materials.MaterialFile, repos.Materials.MaterialChunk, extractor, sagaSvc, bootstrapSvc, repos.Materials.LearningArtifact)
	if err := jobRegistry.Register(ingestChunks); err != nil {
		return Services{}, err
	}

	fileSignatureBuild := file_signature_build.New(
		db,
		log,
		repos.Materials.MaterialFile,
		repos.Materials.MaterialFileSignature,
		repos.Materials.MaterialFileSection,
		repos.Materials.MaterialChunk,
		clients.OpenaiClient,
		clients.PineconeVectorStore,
		sagaSvc,
		bootstrapSvc,
		repos.Materials.LearningArtifact,
	)
	if err := jobRegistry.Register(fileSignatureBuild); err != nil {
		return Services{}, err
	}

	embedChunks := embed_chunks.New(db, log, repos.Materials.MaterialFile, repos.Materials.MaterialChunk, clients.OpenaiClient, clients.PineconeVectorStore, sagaSvc, bootstrapSvc)
	if err := jobRegistry.Register(embedChunks); err != nil {
		return Services{}, err
	}

	materialSummarize := material_set_summarize.New(db, log, repos.Materials.MaterialFile, repos.Materials.MaterialChunk, repos.Materials.MaterialSetSummary, clients.OpenaiClient, clients.PineconeVectorStore, sagaSvc, bootstrapSvc, repos.Materials.LearningArtifact)
	if err := jobRegistry.Register(materialSummarize); err != nil {
		return Services{}, err
	}

	conceptGraph := concept_graph_build.New(db, log, repos.Materials.MaterialFile, repos.Materials.MaterialFileSignature, repos.Materials.MaterialChunk, repos.Paths.Path, repos.Concepts.Concept, repos.Concepts.ConceptRepresentation, repos.Concepts.ConceptMappingOverride, repos.Concepts.ConceptEvidence, repos.Concepts.ConceptEdge, clients.Neo4j, clients.OpenaiClient, clients.PineconeVectorStore, sagaSvc, bootstrapSvc, repos.Materials.LearningArtifact, repos.Concepts.GraphVersion, repos.Concepts.StructuralDecisionTrace)
	if err := jobRegistry.Register(conceptGraph); err != nil {
		return Services{}, err
	}

	conceptGraphPatch := concept_graph_patch_build.New(db, log, repos.Materials.MaterialFile, repos.Materials.MaterialFileSignature, repos.Materials.MaterialChunk, repos.Paths.Path, repos.Concepts.Concept, repos.Concepts.ConceptRepresentation, repos.Concepts.ConceptMappingOverride, repos.Concepts.ConceptEvidence, repos.Concepts.ConceptEdge, clients.Neo4j, clients.OpenaiClient, clients.PineconeVectorStore, sagaSvc, bootstrapSvc, repos.Materials.LearningArtifact, repos.Concepts.GraphVersion, repos.Concepts.StructuralDecisionTrace)
	if err := jobRegistry.Register(conceptGraphPatch); err != nil {
		return Services{}, err
	}

	conceptBridge := concept_bridge_build.New(db, log, repos.Concepts.Concept, repos.Concepts.ConceptEdge, clients.OpenaiClient, clients.PineconeVectorStore, bootstrapSvc)
	if err := jobRegistry.Register(conceptBridge); err != nil {
		return Services{}, err
	}

	materialSignal := material_signal_build.New(
		db,
		log,
		repos.Materials.MaterialFile,
		repos.Materials.MaterialFileSignature,
		repos.Materials.MaterialFileSection,
		repos.Materials.MaterialChunk,
		repos.Concepts.Concept,
		repos.Materials.MaterialSet,
		clients.OpenaiClient,
		bootstrapSvc,
		repos.Materials.LearningArtifact,
	)
	if err := jobRegistry.Register(materialSignal); err != nil {
		return Services{}, err
	}

	materialKG := material_kg_build.New(db, log, repos.Materials.MaterialFile, repos.Materials.MaterialChunk, repos.Paths.Path, repos.Concepts.Concept, clients.Neo4j, clients.OpenaiClient, sagaSvc, bootstrapSvc)
	if err := jobRegistry.Register(materialKG); err != nil {
		return Services{}, err
	}

	conceptCluster := concept_cluster_build.New(db, log, repos.Concepts.Concept, repos.Concepts.ConceptCluster, repos.Concepts.ConceptClusterMember, clients.OpenaiClient, clients.PineconeVectorStore, sagaSvc, bootstrapSvc, repos.Materials.LearningArtifact)
	if err := jobRegistry.Register(conceptCluster); err != nil {
		return Services{}, err
	}

	chainSignatures := chain_signature_build.New(db, log, repos.Concepts.Concept, repos.Concepts.ConceptCluster, repos.Concepts.ConceptClusterMember, repos.Concepts.ConceptEdge, repos.Concepts.ChainSignature, clients.OpenaiClient, clients.PineconeVectorStore, sagaSvc, bootstrapSvc, repos.Materials.LearningArtifact)
	if err := jobRegistry.Register(chainSignatures); err != nil {
		return Services{}, err
	}

	userProfileRefresh := user_profile_refresh.New(db, log, repos.Learning.UserStylePreference, repos.Events.UserProgressionEvent, repos.Users.UserProfileVector, repos.Users.UserPersonalizationPrefs, clients.OpenaiClient, clients.PineconeVectorStore, sagaSvc, bootstrapSvc)
	if err := jobRegistry.Register(userProfileRefresh); err != nil {
		return Services{}, err
	}

	teachingPatterns := teaching_patterns_seed.New(db, log, repos.Activities.TeachingPattern, repos.Users.UserProfileVector, clients.OpenaiClient, clients.PineconeVectorStore, sagaSvc, bootstrapSvc)
	if err := jobRegistry.Register(teachingPatterns); err != nil {
		return Services{}, err
	}

	pathIntake := path_intake.New(
		db,
		log,
		repos.Materials.MaterialFile,
		repos.Materials.MaterialFileSignature,
		repos.Materials.MaterialChunk,
		repos.Materials.MaterialSetSummary,
		repos.Paths.Path,
		repos.Users.UserPersonalizationPrefs,
		repos.Chat.ChatThread,
		repos.Chat.ChatMessage,
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
		repos.Paths.Path,
		repos.Materials.MaterialFile,
		repos.Materials.MaterialFileSignature,
		repos.Users.UserPersonalizationPrefs,
		repos.Chat.ChatThread,
		repos.Chat.ChatMessage,
		chatNotifier,
	)
	if err := jobRegistry.Register(pathGroupingRefine); err != nil {
		return Services{}, err
	}

	pathStructureDispatch := path_structure_dispatch.New(
		db,
		log,
		jobService,
		repos.Jobs.JobRun,
		repos.Paths.Path,
		repos.Materials.MaterialFile,
		repos.Materials.MaterialSet,
		repos.Materials.MaterialSetFile,
		repos.Library.UserLibraryIndex,
	)
	if err := jobRegistry.Register(pathStructureDispatch); err != nil {
		return Services{}, err
	}

	pathStructureRefine := path_structure_refine.New(
		db,
		log,
		repos.Paths.Path,
		repos.Concepts.Concept,
		repos.Materials.MaterialFile,
		repos.Chat.ChatThread,
		repos.Chat.ChatMessage,
		chatNotifier,
	)
	if err := jobRegistry.Register(pathStructureRefine); err != nil {
		return Services{}, err
	}

	pathPlan := path_plan_build.New(db, log, repos.Paths.Path, repos.Paths.PathNode, repos.Concepts.Concept, repos.Concepts.ConceptRepresentation, repos.Concepts.ConceptEdge, repos.Materials.MaterialSetSummary, repos.Users.UserProfileVector, repos.Learning.UserConceptState, repos.Learning.UserConceptModel, repos.Learning.UserMisconception, clients.Neo4j, clients.OpenaiClient, bootstrapSvc)
	if err := jobRegistry.Register(pathPlan); err != nil {
		return Services{}, err
	}

	runtimePlan := runtime_plan_build.New(
		db,
		log,
		repos.Paths.Path,
		repos.Paths.PathNode,
		repos.DocGen.LearningNodeDoc,
		repos.Materials.MaterialSetSummary,
		repos.Users.UserProfileVector,
		repos.Events.UserProgressionEvent,
		clients.OpenaiClient,
		bootstrapSvc,
	)
	if err := jobRegistry.Register(runtimePlan); err != nil {
		return Services{}, err
	}

	psuBuild := psu_build.New(
		db,
		log,
		repos.Paths.PathNode,
		repos.Concepts.Concept,
		repos.Paths.PathStructuralUnit,
		bootstrapSvc,
	)
	if err := jobRegistry.Register(psuBuild); err != nil {
		return Services{}, err
	}

	psuPromote := psu_promote.New(
		db,
		log,
		repos.Events.UserEvent,
		repos.Paths.PathStructuralUnit,
		repos.Concepts.Concept,
		repos.Concepts.ConceptEdge,
		repos.Learning.UserConceptState,
		repos.Learning.UserConceptModel,
		repos.Learning.UserMisconception,
		clients.StructureExtractAI,
	)
	if err := jobRegistry.Register(psuPromote); err != nil {
		return Services{}, err
	}

	structureBackfill := structure_backfill.New(
		db,
		log,
		repos.Paths.Path,
		repos.Paths.PathNode,
		repos.Concepts.Concept,
		repos.Paths.PathStructuralUnit,
		bootstrapSvc,
		repos.Learning.UserConceptState,
		repos.Learning.UserConceptModel,
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
		repos.Jobs.JobRun,
		repos.Paths.Path,
		repos.Paths.PathNode,
		repos.Concepts.ConceptCluster,
		repos.Library.LibraryTaxonomyNode,
		repos.Library.LibraryTaxonomyEdge,
		repos.Library.LibraryTaxonomyMember,
		repos.Library.LibraryTaxonomyState,
		repos.Library.LibraryTaxonomySnapshot,
		repos.Library.LibraryPathEmbedding,
	)
	if err := jobRegistry.Register(libraryTaxonomyRoute); err != nil {
		return Services{}, err
	}

	libraryTaxonomyRefine := library_taxonomy_refine.New(
		db,
		log,
		clients.OpenaiClient,
		clients.Neo4j,
		repos.Paths.Path,
		repos.Paths.PathNode,
		repos.Concepts.ConceptCluster,
		repos.Library.LibraryTaxonomyNode,
		repos.Library.LibraryTaxonomyEdge,
		repos.Library.LibraryTaxonomyMember,
		repos.Library.LibraryTaxonomyState,
		repos.Library.LibraryTaxonomySnapshot,
		repos.Library.LibraryPathEmbedding,
	)
	if err := jobRegistry.Register(libraryTaxonomyRefine); err != nil {
		return Services{}, err
	}

	pathCoverRender := path_cover_render.New(
		db,
		log,
		repos.Paths.Path,
		repos.Paths.PathNode,
		avatarService,
		bootstrapSvc,
	)
	if err := jobRegistry.Register(pathCoverRender); err != nil {
		return Services{}, err
	}

	nodeAvatarRender := node_avatar_render.New(
		db,
		log,
		repos.Paths.Path,
		repos.Paths.PathNode,
		avatarService,
		bootstrapSvc,
	)
	if err := jobRegistry.Register(nodeAvatarRender); err != nil {
		return Services{}, err
	}

	nodeFiguresPlan := node_figures_plan_build.New(
		db,
		log,
		repos.Paths.Path,
		repos.Paths.PathNode,
		repos.DocGen.LearningNodeFigure,
		repos.DocGen.DocGenerationRun,
		repos.Materials.MaterialFile,
		repos.Materials.MaterialChunk,
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
		repos.Paths.Path,
		repos.Paths.PathNode,
		repos.DocGen.LearningNodeFigure,
		repos.Materials.Asset,
		repos.DocGen.DocGenerationRun,
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
		repos.Paths.Path,
		repos.Paths.PathNode,
		repos.DocGen.LearningNodeVideo,
		repos.DocGen.DocGenerationRun,
		repos.Materials.MaterialFile,
		repos.Materials.MaterialChunk,
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
		repos.Paths.Path,
		repos.Paths.PathNode,
		repos.DocGen.LearningNodeVideo,
		repos.Materials.Asset,
		repos.DocGen.DocGenerationRun,
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
		repos.Paths.Path,
		repos.Paths.PathNode,
		repos.DocGen.LearningNodeDoc,
		repos.DocGen.LearningNodeFigure,
		repos.DocGen.LearningNodeVideo,
		repos.DocGen.DocGenerationRun,
		repos.DocGen.LearningNodeDocBlueprint,
		repos.DocGen.DocRetrievalPack,
		repos.DocGen.DocGenerationTrace,
		repos.DocGen.DocConstraintReport,
		repos.DocGen.LearningNodeDocRevision,
		repos.Materials.MaterialFile,
		repos.Materials.MaterialChunk,
		repos.Users.UserProfileVector,
		repos.Activities.TeachingPattern,
		repos.Concepts.Concept,
		repos.Learning.UserConceptState,
		repos.Learning.UserConceptModel,
		repos.Learning.UserMisconception,
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
		repos.Paths.Path,
		repos.Paths.PathNode,
		repos.DocGen.LearningNodeDoc,
		repos.DocGen.LearningNodeFigure,
		repos.DocGen.LearningNodeVideo,
		repos.DocGen.DocGenerationRun,
		repos.DocGen.LearningNodeDocBlueprint,
		repos.DocGen.DocRetrievalPack,
		repos.DocGen.DocGenerationTrace,
		repos.DocGen.DocConstraintReport,
		repos.DocGen.LearningNodeDocRevision,
		repos.Materials.MaterialFile,
		repos.Materials.MaterialChunk,
		repos.Users.UserProfileVector,
		repos.Activities.TeachingPattern,
		repos.Concepts.Concept,
		repos.Learning.UserConceptState,
		repos.Learning.UserConceptModel,
		repos.Learning.UserMisconception,
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
		repos.Paths.Path,
		repos.Paths.PathRun,
		repos.Paths.NodeRun,
		repos.Paths.PathNode,
		repos.DocGen.LearningNodeDoc,
		repos.DocGen.LearningNodeDocVariant,
		repos.DocGen.UserDocSignalSnapshot,
		repos.Learning.InterventionPlan,
		repos.DocGen.LearningNodeFigure,
		repos.DocGen.LearningNodeVideo,
		repos.DocGen.DocGenerationRun,
		repos.DocGen.LearningNodeDocBlueprint,
		repos.DocGen.DocRetrievalPack,
		repos.DocGen.DocGenerationTrace,
		repos.DocGen.DocConstraintReport,
		repos.DocGen.LearningNodeDocRevision,
		repos.Materials.MaterialFile,
		repos.Materials.MaterialChunk,
		repos.Users.UserProfileVector,
		repos.Activities.TeachingPattern,
		repos.Concepts.Concept,
		repos.Learning.UserConceptState,
		repos.Learning.UserConceptModel,
		repos.Learning.UserMisconception,
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
		repos.Paths.Path,
		repos.Paths.PathNode,
		repos.DocGen.LearningNodeDoc,
		repos.DocGen.LearningNodeFigure,
		repos.DocGen.LearningNodeVideo,
		repos.DocGen.LearningNodeDocRevision,
		repos.Materials.MaterialFile,
		repos.Materials.MaterialChunk,
		repos.Library.UserLibraryIndex,
		repos.Materials.Asset,
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
		repos.Chat.ChatThread,
		repos.Chat.ChatMessage,
		repos.Paths.Path,
		repos.Paths.PathNode,
		repos.DocGen.LearningNodeDoc,
		repos.DocGen.LearningNodeFigure,
		repos.DocGen.LearningNodeVideo,
		repos.Materials.MaterialFile,
		repos.Materials.MaterialChunk,
		repos.Library.UserLibraryIndex,
		repos.Materials.Asset,
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
		repos.Jobs.JobRun,
		jobService,
		repos.Chat.ChatThread,
		repos.Paths.PathNode,
		repos.DocGen.LearningNodeDoc,
		repos.DocGen.LearningNodeDocRevision,
	)
	if err := jobRegistry.Register(nodeDocEditApply); err != nil {
		return Services{}, err
	}

	realizeActivities := realize_activities.New(
		db,
		log,
		repos.Paths.Path,
		repos.Paths.PathNode,
		repos.Paths.PathNodeActivity,
		repos.Activities.Activity,
		repos.Activities.ActivityVariant,
		repos.Activities.ActivityConcept,
		repos.Activities.ActivityCitation,
		repos.Concepts.Concept,
		repos.Learning.UserConceptState,
		repos.Learning.UserConceptModel,
		repos.Learning.UserMisconception,
		repos.Materials.MaterialFile,
		repos.Materials.MaterialChunk,
		repos.Users.UserProfileVector,
		repos.Activities.TeachingPattern,
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

	coverageAudit := coverage_coherence_audit.New(db, log, repos.Paths.Path, repos.Paths.PathNode, repos.Concepts.Concept, repos.Activities.Activity, repos.Activities.ActivityVariant, clients.OpenaiClient, bootstrapSvc)
	if err := jobRegistry.Register(coverageAudit); err != nil {
		return Services{}, err
	}

	progressionCompact := progression_compact.New(db, log, repos.Events.UserEvent, repos.Events.UserEventCursor, repos.Events.UserProgressionEvent, bootstrapSvc)
	if err := jobRegistry.Register(progressionCompact); err != nil {
		return Services{}, err
	}

	docProbeSelect := doc_probe_select.New(
		db,
		log,
		repos.Paths.Path,
		repos.Paths.PathRun,
		repos.Paths.PathNode,
		repos.DocGen.LearningNodeDoc,
		repos.DocGen.LearningNodeDocVariant,
		repos.Concepts.Concept,
		repos.Learning.UserConceptState,
		repos.Learning.UserMisconception,
		repos.Learning.UserTestletState,
		repos.DocGen.DocProbe,
		bootstrapSvc,
	)
	if err := jobRegistry.Register(docProbeSelect); err != nil {
		return Services{}, err
	}

	runtimeUpdate := runtime_update.New(
		db,
		log,
		repos.Events.UserEvent,
		repos.Events.UserEventCursor,
		repos.Paths.Path,
		repos.Paths.PathNode,
		repos.Paths.PathNodeActivity,
		repos.DocGen.LearningNodeDoc,
		repos.Paths.PathRun,
		repos.Paths.NodeRun,
		repos.Paths.ActivityRun,
		repos.Paths.PathRunTransition,
		repos.Users.UserSessionState,
		repos.Concepts.Concept,
		repos.Concepts.ConceptEdge,
		repos.Learning.UserConceptState,
		repos.Learning.UserConceptModel,
		repos.Learning.UserMisconception,
		repos.Learning.MisconceptionCausalEdge,
		repos.Learning.MisconceptionResolution,
		repos.Learning.UserBeliefSnapshot,
		repos.Learning.InterventionPlan,
		repos.Learning.ConceptReadinessSnapshot,
		repos.Learning.PrereqGateDecision,
		repos.Learning.UserTestletState,
		repos.DocGen.DocProbe,
		repos.DocGen.DocProbeOutcome,
		repos.Runtime.DecisionTrace,
		repos.Runtime.ModelSnapshot,
		repos.Runtime.PolicyEvalSnapshot,
		jobService,
		runtimeNotifier,
		metrics,
	)
	if err := jobRegistry.Register(runtimeUpdate); err != nil {
		return Services{}, err
	}

	policyEval := policy_eval_refresh.New(db, log, repos.Runtime.DecisionTrace, repos.Runtime.PolicyEvalSnapshot)
	if err := jobRegistry.Register(policyEval); err != nil {
		return Services{}, err
	}

	driftMonitor := structural_drift_monitor.New(db, log, repos.Concepts.StructuralDriftMetric, repos.Concepts.RollbackEvent)
	if err := jobRegistry.Register(driftMonitor); err != nil {
		return Services{}, err
	}

	traceBackfill := structural_trace_backfill.New(db, log)
	if err := jobRegistry.Register(traceBackfill); err != nil {
		return Services{}, err
	}

	traceCompact := trace_compact.New(db, log)
	if err := jobRegistry.Register(traceCompact); err != nil {
		return Services{}, err
	}

	graphRollback := graph_version_rollback.New(db, log, repos.Concepts.GraphVersion, repos.Concepts.RollbackEvent, repos.Jobs.JobRun, jobService)
	if err := jobRegistry.Register(graphRollback); err != nil {
		return Services{}, err
	}

	traceLoadTest := trace_load_test.New(db, log, metrics)
	if err := jobRegistry.Register(traceLoadTest); err != nil {
		return Services{}, err
	}

	policyTrain := policy_model_train.New(db, log, repos.Runtime.DecisionTrace, repos.Runtime.ModelSnapshot)
	if err := jobRegistry.Register(policyTrain); err != nil {
		return Services{}, err
	}

	variantStats := variant_stats_refresh.New(db, log, repos.Events.UserEvent, repos.Events.UserEventCursor, repos.Activities.ActivityVariant, repos.Activities.ActivityVariantStat, bootstrapSvc)
	if err := jobRegistry.Register(variantStats); err != nil {
		return Services{}, err
	}

	docVariantEval := doc_variant_eval.New(
		db,
		log,
		repos.DocGen.DocVariantExposure,
		repos.DocGen.DocVariantOutcome,
		repos.Paths.NodeRun,
		repos.Learning.UserConceptState,
		bootstrapSvc,
	)
	if err := jobRegistry.Register(docVariantEval); err != nil {
		return Services{}, err
	}

	priors := priors_refresh.New(
		db,
		log,
		repos.Activities.Activity,
		repos.Activities.ActivityVariant,
		repos.Activities.ActivityVariantStat,
		repos.Concepts.ChainSignature,
		repos.Concepts.Concept,
		repos.Activities.ActivityConcept,
		repos.Concepts.ChainPrior,
		repos.Concepts.CohortPrior,
		bootstrapSvc,
	)
	if err := jobRegistry.Register(priors); err != nil {
		return Services{}, err
	}

	completedUnits := completed_unit_refresh.New(
		db,
		log,
		repos.Activities.UserCompletedUnit,
		repos.Events.UserProgressionEvent,
		repos.Concepts.Concept,
		repos.Activities.Activity,
		repos.Activities.ActivityConcept,
		repos.Concepts.ChainSignature,
		repos.Learning.UserConceptState,
		clients.Neo4j,
		bootstrapSvc,
	)
	if err := jobRegistry.Register(completedUnits); err != nil {
		return Services{}, err
	}

	sagaCleanup := saga_cleanup.New(db, log, repos.Jobs.SagaRun, sagaSvc, clients.GcpBucket)
	if err := jobRegistry.Register(sagaCleanup); err != nil {
		return Services{}, err
	}

	learningBuild := learning_build.New(
		db,
		log,
		jobService,
		repos.Paths.Path,
		repos.Chat.ChatThread,
		repos.Chat.ChatMessage,
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

			Files:        repos.Materials.MaterialFile,
			FileSigs:     repos.Materials.MaterialFileSignature,
			FileSections: repos.Materials.MaterialFileSection,
			Chunks:       repos.Materials.MaterialChunk,
			MaterialSets: repos.Materials.MaterialSet,
			Summaries:    repos.Materials.MaterialSetSummary,

			Concepts:         repos.Concepts.Concept,
			ConceptReps:      repos.Concepts.ConceptRepresentation,
			MappingOverrides: repos.Concepts.ConceptMappingOverride,
			Evidence:         repos.Concepts.ConceptEvidence,
			Edges:            repos.Concepts.ConceptEdge,

			Clusters: repos.Concepts.ConceptCluster,
			Members:  repos.Concepts.ConceptClusterMember,

			ChainSignatures:     repos.Concepts.ChainSignature,
			PathStructuralUnits: repos.Paths.PathStructuralUnit,

			StylePrefs:       repos.Learning.UserStylePreference,
			ConceptState:     repos.Learning.UserConceptState,
			ConceptModel:     repos.Learning.UserConceptModel,
			MisconRepo:       repos.Learning.UserMisconception,
			ProgEvents:       repos.Events.UserProgressionEvent,
			UserProfile:      repos.Users.UserProfileVector,
			UserPrefs:        repos.Users.UserPersonalizationPrefs,
			TeachingPatterns: repos.Activities.TeachingPattern,

			Path:                 repos.Paths.Path,
			PathNodes:            repos.Paths.PathNode,
			PathNodeActivities:   repos.Paths.PathNodeActivity,
			NodeDocs:             repos.DocGen.LearningNodeDoc,
			NodeDocRevisions:     repos.DocGen.LearningNodeDocRevision,
			NodeDocBlueprints:    repos.DocGen.LearningNodeDocBlueprint,
			NodeFigures:          repos.DocGen.LearningNodeFigure,
			NodeVideos:           repos.DocGen.LearningNodeVideo,
			DocGenRuns:           repos.DocGen.DocGenerationRun,
			DocRetrievalPacks:    repos.DocGen.DocRetrievalPack,
			DocGenerationTraces:  repos.DocGen.DocGenerationTrace,
			DocConstraintReports: repos.DocGen.DocConstraintReport,
			Assets:               repos.Materials.Asset,
			Artifacts:            repos.Materials.LearningArtifact,

			Activities:        repos.Activities.Activity,
			Variants:          repos.Activities.ActivityVariant,
			ActivityConcepts:  repos.Activities.ActivityConcept,
			ActivityCitations: repos.Activities.ActivityCitation,

			UserEvents:            repos.Events.UserEvent,
			UserEventCursors:      repos.Events.UserEventCursor,
			UserProgressionEvents: repos.Events.UserProgressionEvent,
			VariantStats:          repos.Activities.ActivityVariantStat,

			ChainPriors:    repos.Concepts.ChainPrior,
			CohortPriors:   repos.Concepts.CohortPrior,
			CompletedUnits: repos.Activities.UserCompletedUnit,
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
		repos.Paths.Path,
		repos.Chat.ChatThread,
		repos.Chat.ChatMessage,
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

			Files:        repos.Materials.MaterialFile,
			FileSigs:     repos.Materials.MaterialFileSignature,
			FileSections: repos.Materials.MaterialFileSection,
			Chunks:       repos.Materials.MaterialChunk,
			MaterialSets: repos.Materials.MaterialSet,
			Summaries:    repos.Materials.MaterialSetSummary,

			Concepts:         repos.Concepts.Concept,
			ConceptReps:      repos.Concepts.ConceptRepresentation,
			MappingOverrides: repos.Concepts.ConceptMappingOverride,
			Evidence:         repos.Concepts.ConceptEvidence,
			Edges:            repos.Concepts.ConceptEdge,

			Clusters: repos.Concepts.ConceptCluster,
			Members:  repos.Concepts.ConceptClusterMember,

			ChainSignatures:     repos.Concepts.ChainSignature,
			PathStructuralUnits: repos.Paths.PathStructuralUnit,

			StylePrefs:       repos.Learning.UserStylePreference,
			ConceptState:     repos.Learning.UserConceptState,
			ConceptModel:     repos.Learning.UserConceptModel,
			MisconRepo:       repos.Learning.UserMisconception,
			ProgEvents:       repos.Events.UserProgressionEvent,
			UserProfile:      repos.Users.UserProfileVector,
			UserPrefs:        repos.Users.UserPersonalizationPrefs,
			TeachingPatterns: repos.Activities.TeachingPattern,

			Path:                 repos.Paths.Path,
			PathNodes:            repos.Paths.PathNode,
			PathNodeActivities:   repos.Paths.PathNodeActivity,
			NodeDocs:             repos.DocGen.LearningNodeDoc,
			NodeDocRevisions:     repos.DocGen.LearningNodeDocRevision,
			NodeDocBlueprints:    repos.DocGen.LearningNodeDocBlueprint,
			NodeFigures:          repos.DocGen.LearningNodeFigure,
			NodeVideos:           repos.DocGen.LearningNodeVideo,
			DocGenRuns:           repos.DocGen.DocGenerationRun,
			DocRetrievalPacks:    repos.DocGen.DocRetrievalPack,
			DocGenerationTraces:  repos.DocGen.DocGenerationTrace,
			DocConstraintReports: repos.DocGen.DocConstraintReport,
			Assets:               repos.Materials.Asset,
			Artifacts:            repos.Materials.LearningArtifact,

			Activities:        repos.Activities.Activity,
			Variants:          repos.Activities.ActivityVariant,
			ActivityConcepts:  repos.Activities.ActivityConcept,
			ActivityCitations: repos.Activities.ActivityCitation,

			UserEvents:            repos.Events.UserEvent,
			UserEventCursors:      repos.Events.UserEventCursor,
			UserProgressionEvents: repos.Events.UserProgressionEvent,
			VariantStats:          repos.Activities.ActivityVariantStat,

			ChainPriors:    repos.Concepts.ChainPrior,
			CohortPriors:   repos.Concepts.CohortPrior,
			CompletedUnits: repos.Activities.UserCompletedUnit,
		},
		metrics,
	)
	if err := jobRegistry.Register(learningBuildProgressive); err != nil {
		return Services{}, err
	}

	userModel := user_model_update.New(
		db,
		log,
		repos.Events.UserEvent,
		repos.Events.UserEventCursor,
		repos.Concepts.Concept,
		repos.Concepts.ConceptEdge,
		repos.Activities.ActivityConcept,
		repos.Learning.UserConceptState,
		repos.Learning.UserConceptModel,
		repos.Learning.UserConceptEdgeStat,
		repos.Learning.UserConceptEvidence,
		repos.Learning.UserConceptCalibration,
		repos.Learning.ItemCalibration,
		repos.Learning.UserModelAlert,
		repos.Learning.UserMisconception,
		repos.Learning.MisconceptionCausalEdge,
		repos.Learning.UserStylePreference,
		repos.Concepts.ConceptClusterMember,
		repos.Learning.UserTestletState,
		repos.Learning.UserSkillState,
		clients.Neo4j,
		repos.Jobs.JobRun,
		jobService,
	)
	if err := jobRegistry.Register(userModel); err != nil {
		return Services{}, err
	}

	var temporalRunner *temporalworker.Runner
	if runWorker {
		w, err := temporalworker.NewRunner(log, clients.Temporal, db, repos.Jobs.JobRun, jobRegistry, jobNotifier, metrics)
		if err != nil {
			return Services{}, fmt.Errorf("init temporal worker: %w", err)
		}
		temporalRunner = w
	}

	chatService := services.NewChatService(
		db,
		log,
		repos.Paths.Path,
		repos.Jobs.JobRun,
		jobService,
		repos.Chat.ChatThread,
		repos.Chat.ChatMessage,
		repos.Chat.ChatTurn,
		chatNotifier,
		repos.Users.UserSessionState,
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
