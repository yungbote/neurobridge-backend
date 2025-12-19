package app

import (
	"fmt"
	"os"
	"strings"

	"gorm.io/gorm"

	ingestion "github.com/yungbote/neurobridge-backend/internal/ingestion/pipeline"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/chain_signature_build"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/completed_unit_refresh"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/concept_cluster_build"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/concept_graph_build"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/course_build"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/coverage_coherence_audit"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/embed_chunks"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/ingest_chunks"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/learning_build"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/material_set_summarize"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/path_plan_build"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/priors_refresh"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/progression_compact"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/realize_activities"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/saga_cleanup"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/teaching_patterns_seed"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/user_model_update"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/user_profile_refresh"
	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/variant_stats_refresh"
	jobruntime "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
	jobworker "github.com/yungbote/neurobridge-backend/internal/jobs/worker"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"github.com/yungbote/neurobridge-backend/internal/realtime"
	"github.com/yungbote/neurobridge-backend/internal/realtime/bus"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type Services struct {
	// Core
	Avatar services.AvatarService
	File   services.FileService

	// Auth + domain
	Auth     services.AuthService
	User     services.UserService
	Material services.MaterialService
	Course   services.CourseService
	Module   services.ModuleService
	Lesson   services.LessonService

	// User Event Ingestion (raw user_event log)
	Events services.EventService

	// Jobs + notifications
	JobNotifier    services.JobNotifier
	JobService     services.JobService
	Workflow       services.WorkflowService
	CourseNotifier services.CourseNotifier

	// Orchestrator
	ContentExtractor ingestion.ContentExtractionService

	// Job infra
	JobRegistry *jobruntime.Registry
	JobWorker   *jobworker.Worker

	// Keep bus here for convenience/compat
	SSEBus bus.Bus
}

func wireServices(db *gorm.DB, log *logger.Logger, cfg Config, repos Repos, sseHub *realtime.SSEHub, clients Clients) (Services, error) {
	log.Info("Wiring services...")

	avatarService, err := services.NewAvatarService(db, log, repos.User, clients.GcpBucket)
	if err != nil {
		return Services{}, fmt.Errorf("init avatar service: %w", err)
	}

	fileService := services.NewFileService(db, log, clients.GcpBucket, repos.MaterialFile)

	authService := services.NewAuthService(
		db, log,
		repos.User,
		avatarService,
		repos.UserToken,
		cfg.JWTSecretKey,
		cfg.AccessTokenTTL,
		cfg.RefreshTokenTTL,
	)

	userService := services.NewUserService(db, log, repos.User)
	materialService := services.NewMaterialService(db, log, repos.MaterialSet, repos.MaterialFile, fileService)
	courseService := services.NewCourseService(db, log, repos.Course, repos.MaterialSet)
	moduleService := services.NewModuleService(db, log, repos.Course, repos.CourseModule)
	lessonService := services.NewLessonService(db, log, repos.Course, repos.CourseModule, repos.Lesson)
	eventService := services.NewEventService(db, log, repos.UserEvent)

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
	jobService := services.NewJobService(db, log, repos.JobRun, jobNotifier)
	workflow := services.NewWorkflowService(db, log, materialService, jobService)
	courseNotifier := services.NewCourseNotifier(emitter)

	extractor := ingestion.NewContentExtractionService(
		db,
		log,
		repos.MaterialChunk,
		repos.MaterialFile,
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

	// Shared learning_build services (durable saga + path bootstrap).
	sagaSvc := services.NewSagaService(db, log, repos.SagaRun, repos.SagaAction, clients.GcpBucket, clients.PineconeVectorStore)
	bootstrapSvc := services.NewLearningBuildBootstrapService(db, log, repos.Path, repos.UserLibraryIndex)

	// --------------------
	// Learning build (Path-centric) pipelines
	// --------------------
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

	conceptGraph := concept_graph_build.New(db, log, repos.MaterialFile, repos.MaterialChunk, repos.Concept, repos.ConceptEvidence, repos.ConceptEdge, clients.OpenaiClient, clients.PineconeVectorStore, sagaSvc, bootstrapSvc)
	if err := jobRegistry.Register(conceptGraph); err != nil {
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

	userProfileRefresh := user_profile_refresh.New(db, log, repos.UserStylePreference, repos.UserProgressionEvent, repos.UserProfileVector, clients.OpenaiClient, clients.PineconeVectorStore, sagaSvc, bootstrapSvc)
	if err := jobRegistry.Register(userProfileRefresh); err != nil {
		return Services{}, err
	}

	teachingPatterns := teaching_patterns_seed.New(db, log, repos.TeachingPattern, repos.UserProfileVector, clients.OpenaiClient, clients.PineconeVectorStore, sagaSvc, bootstrapSvc)
	if err := jobRegistry.Register(teachingPatterns); err != nil {
		return Services{}, err
	}

	pathPlan := path_plan_build.New(db, log, repos.Path, repos.PathNode, repos.Concept, repos.ConceptEdge, repos.MaterialSetSummary, repos.UserProfileVector, clients.OpenaiClient, bootstrapSvc)
	if err := jobRegistry.Register(pathPlan); err != nil {
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
		repos.MaterialFile,
		repos.MaterialChunk,
		repos.UserProfileVector,
		clients.OpenaiClient,
		clients.PineconeVectorStore,
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
		sagaSvc,
		bootstrapSvc,
		&learning_build.InlineDeps{
			Extract: extractor,
			AI:      clients.OpenaiClient,
			Vec:     clients.PineconeVectorStore,

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
			TeachingPatterns: repos.TeachingPattern,

			Path:               repos.Path,
			PathNodes:          repos.PathNode,
			PathNodeActivities: repos.PathNodeActivity,

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

	courseBuild := course_build.NewCourseBuildPipeline(
		db,
		log,
		repos.Course,
		repos.MaterialFile,
		repos.CourseModule,
		repos.Lesson,
		repos.QuizQuestion,
		repos.CourseBlueprint,
		repos.MaterialChunk,
		clients.GcpBucket,
		clients.OpenaiClient,
		courseNotifier,
		extractor,
		clients.PineconeVectorStore,
	)
	if err := jobRegistry.Register(courseBuild); err != nil {
		return Services{}, err
	}

	userModel := user_model_update.New(
		db,
		log,
		repos.UserEvent,
		repos.UserEventCursor,
		repos.UserConceptState,
		repos.UserStylePreference,
		repos.JobRun,
	)
	if err := jobRegistry.Register(userModel); err != nil {
		return Services{}, err
	}

	var worker *jobworker.Worker
	if runWorker {
		worker = jobworker.NewWorker(db, log, repos.JobRun, jobRegistry, jobNotifier)
	}

	return Services{
		Avatar:           avatarService,
		File:             fileService,
		Auth:             authService,
		User:             userService,
		Material:         materialService,
		Course:           courseService,
		Module:           moduleService,
		Lesson:           lessonService,
		Events:           eventService,
		JobNotifier:      jobNotifier,
		JobService:       jobService,
		Workflow:         workflow,
		CourseNotifier:   courseNotifier,
		ContentExtractor: extractor,
		JobRegistry:      jobRegistry,
		JobWorker:        worker,
		SSEBus:           clients.SSEBus,
	}, nil
}
