package learning

import (
	"context"

	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	ingestion "github.com/yungbote/neurobridge-backend/internal/modules/learning/ingestion/pipeline"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/steps"
	"github.com/yungbote/neurobridge-backend/internal/platform/gcp"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/neo4jdb"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
	"github.com/yungbote/neurobridge-backend/internal/platform/pinecone"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type UsecasesDeps struct {
	DB  *gorm.DB
	Log *logger.Logger

	Extract ingestion.ContentExtractionService

	AI    openai.Client
	Vec   pinecone.VectorStore
	Graph *neo4jdb.Client

	Bucket gcp.BucketService
	Avatar services.AvatarService

	Files        repos.MaterialFileRepo
	Chunks       repos.MaterialChunkRepo
	MaterialSets repos.MaterialSetRepo
	Summaries    repos.MaterialSetSummaryRepo

	Path               repos.PathRepo
	PathNodes          repos.PathNodeRepo
	PathNodeActivities repos.PathNodeActivityRepo

	Concepts repos.ConceptRepo
	Evidence repos.ConceptEvidenceRepo
	Edges    repos.ConceptEdgeRepo

	Clusters repos.ConceptClusterRepo
	Members  repos.ConceptClusterMemberRepo

	ChainSignatures repos.ChainSignatureRepo

	StylePrefs  repos.UserStylePreferenceRepo
	ProgEvents  repos.UserProgressionEventRepo
	Prefs       repos.UserPersonalizationPrefsRepo
	UserProfile repos.UserProfileVectorRepo

	TeachingPatterns repos.TeachingPatternRepo

	NodeDocs  repos.LearningNodeDocRepo
	Figures   repos.LearningNodeFigureRepo
	Videos    repos.LearningNodeVideoRepo
	Revisions repos.LearningNodeDocRevisionRepo
	GenRuns   repos.LearningDocGenerationRunRepo
	Drills    repos.LearningDrillInstanceRepo

	Assets repos.AssetRepo
	ULI    repos.UserLibraryIndexRepo

	Activities        repos.ActivityRepo
	Variants          repos.ActivityVariantRepo
	ActivityConcepts  repos.ActivityConceptRepo
	ActivityCitations repos.ActivityCitationRepo

	UserEvents       repos.UserEventRepo
	UserEventCursors repos.UserEventCursorRepo
	VariantStats     repos.ActivityVariantStatRepo

	ChainPriors    repos.ChainPriorRepo
	CohortPriors   repos.CohortPriorRepo
	CompletedUnits repos.UserCompletedUnitRepo
	ConceptState   repos.UserConceptStateRepo

	Sagas repos.SagaRunRepo

	Threads  repos.ChatThreadRepo
	Messages repos.ChatMessageRepo
	Notify   services.ChatNotifier

	Saga      services.SagaService
	Bootstrap services.LearningBuildBootstrapService
}

type Usecases struct {
	deps UsecasesDeps
}

func New(deps UsecasesDeps) Usecases { return Usecases{deps: deps} }

func (u Usecases) WithLog(log *logger.Logger) Usecases {
	u.deps.Log = log
	return u
}

type (
	WebResourcesSeedInput  = steps.WebResourcesSeedInput
	WebResourcesSeedOutput = steps.WebResourcesSeedOutput

	IngestChunksInput   = steps.IngestChunksInput
	IngestChunksOutput  = steps.IngestChunksOutput
	IngestChunksOptions = steps.IngestChunksOptions

	EmbedChunksInput  = steps.EmbedChunksInput
	EmbedChunksOutput = steps.EmbedChunksOutput

	MaterialSetSummarizeInput  = steps.MaterialSetSummarizeInput
	MaterialSetSummarizeOutput = steps.MaterialSetSummarizeOutput

	PathIntakeInput  = steps.PathIntakeInput
	PathIntakeOutput = steps.PathIntakeOutput

	UserProfileRefreshInput  = steps.UserProfileRefreshInput
	UserProfileRefreshOutput = steps.UserProfileRefreshOutput

	TeachingPatternsSeedInput  = steps.TeachingPatternsSeedInput
	TeachingPatternsSeedOutput = steps.TeachingPatternsSeedOutput

	ConceptGraphBuildInput  = steps.ConceptGraphBuildInput
	ConceptGraphBuildOutput = steps.ConceptGraphBuildOutput

	MaterialKGBuildInput  = steps.MaterialKGBuildInput
	MaterialKGBuildOutput = steps.MaterialKGBuildOutput

	ConceptClusterBuildInput  = steps.ConceptClusterBuildInput
	ConceptClusterBuildOutput = steps.ConceptClusterBuildOutput

	ChainSignatureBuildInput  = steps.ChainSignatureBuildInput
	ChainSignatureBuildOutput = steps.ChainSignatureBuildOutput

	PathPlanBuildInput  = steps.PathPlanBuildInput
	PathPlanBuildOutput = steps.PathPlanBuildOutput

	PathCoverRenderInput  = steps.PathCoverRenderInput
	PathCoverRenderOutput = steps.PathCoverRenderOutput

	NodeAvatarRenderInput  = steps.NodeAvatarRenderInput
	NodeAvatarRenderOutput = steps.NodeAvatarRenderOutput

	NodeFiguresPlanBuildInput  = steps.NodeFiguresPlanBuildInput
	NodeFiguresPlanBuildOutput = steps.NodeFiguresPlanBuildOutput

	NodeFiguresRenderInput  = steps.NodeFiguresRenderInput
	NodeFiguresRenderOutput = steps.NodeFiguresRenderOutput

	NodeVideosPlanBuildInput  = steps.NodeVideosPlanBuildInput
	NodeVideosPlanBuildOutput = steps.NodeVideosPlanBuildOutput

	NodeVideosRenderInput  = steps.NodeVideosRenderInput
	NodeVideosRenderOutput = steps.NodeVideosRenderOutput

	NodeContentBuildInput  = steps.NodeContentBuildInput
	NodeContentBuildOutput = steps.NodeContentBuildOutput

	NodeDocBuildInput  = steps.NodeDocBuildInput
	NodeDocBuildOutput = steps.NodeDocBuildOutput

	NodeDocPatchSelection = steps.NodeDocPatchSelection
	NodeDocPatchInput     = steps.NodeDocPatchInput
	NodeDocPatchOutput    = steps.NodeDocPatchOutput

	RealizeActivitiesInput  = steps.RealizeActivitiesInput
	RealizeActivitiesOutput = steps.RealizeActivitiesOutput

	CoverageCoherenceAuditInput  = steps.CoverageCoherenceAuditInput
	CoverageCoherenceAuditOutput = steps.CoverageCoherenceAuditOutput

	ProgressionCompactInput  = steps.ProgressionCompactInput
	ProgressionCompactOutput = steps.ProgressionCompactOutput

	VariantStatsRefreshInput  = steps.VariantStatsRefreshInput
	VariantStatsRefreshOutput = steps.VariantStatsRefreshOutput

	PriorsRefreshInput  = steps.PriorsRefreshInput
	PriorsRefreshOutput = steps.PriorsRefreshOutput

	CompletedUnitRefreshInput  = steps.CompletedUnitRefreshInput
	CompletedUnitRefreshOutput = steps.CompletedUnitRefreshOutput

	SagaCleanupInput  = steps.SagaCleanupInput
	SagaCleanupOutput = steps.SagaCleanupOutput
)

func (u Usecases) WebResourcesSeed(ctx context.Context, in WebResourcesSeedInput) (WebResourcesSeedOutput, error) {
	return steps.WebResourcesSeed(ctx, steps.WebResourcesSeedDeps{
		DB:        u.deps.DB,
		Log:       u.deps.Log,
		Files:     u.deps.Files,
		Path:      u.deps.Path,
		Bucket:    u.deps.Bucket,
		Threads:   u.deps.Threads,
		Messages:  u.deps.Messages,
		Notify:    u.deps.Notify,
		AI:        u.deps.AI,
		Saga:      u.deps.Saga,
		Bootstrap: u.deps.Bootstrap,
	}, steps.WebResourcesSeedInput(in))
}

func (u Usecases) IngestChunks(ctx context.Context, in IngestChunksInput, opts ...IngestChunksOptions) (IngestChunksOutput, error) {
	return steps.IngestChunks(ctx, steps.IngestChunksDeps{
		DB:        u.deps.DB,
		Log:       u.deps.Log,
		Files:     u.deps.Files,
		Chunks:    u.deps.Chunks,
		Extract:   u.deps.Extract,
		Saga:      u.deps.Saga,
		Bootstrap: u.deps.Bootstrap,
	}, steps.IngestChunksInput(in), opts...)
}

func (u Usecases) EmbedChunks(ctx context.Context, in EmbedChunksInput) (EmbedChunksOutput, error) {
	return steps.EmbedChunks(ctx, steps.EmbedChunksDeps{
		DB:        u.deps.DB,
		Log:       u.deps.Log,
		Files:     u.deps.Files,
		Chunks:    u.deps.Chunks,
		AI:        u.deps.AI,
		Vec:       u.deps.Vec,
		Saga:      u.deps.Saga,
		Bootstrap: u.deps.Bootstrap,
	}, steps.EmbedChunksInput(in))
}

func (u Usecases) MaterialSetSummarize(ctx context.Context, in MaterialSetSummarizeInput) (MaterialSetSummarizeOutput, error) {
	return steps.MaterialSetSummarize(ctx, steps.MaterialSetSummarizeDeps{
		DB:        u.deps.DB,
		Log:       u.deps.Log,
		Files:     u.deps.Files,
		Chunks:    u.deps.Chunks,
		Summaries: u.deps.Summaries,
		AI:        u.deps.AI,
		Vec:       u.deps.Vec,
		Saga:      u.deps.Saga,
		Bootstrap: u.deps.Bootstrap,
	}, steps.MaterialSetSummarizeInput(in))
}

func (u Usecases) PathIntake(ctx context.Context, in PathIntakeInput) (PathIntakeOutput, error) {
	return steps.PathIntake(ctx, steps.PathIntakeDeps{
		DB:        u.deps.DB,
		Log:       u.deps.Log,
		Files:     u.deps.Files,
		Chunks:    u.deps.Chunks,
		Summaries: u.deps.Summaries,
		Path:      u.deps.Path,
		Prefs:     u.deps.Prefs,
		Threads:   u.deps.Threads,
		Messages:  u.deps.Messages,
		AI:        u.deps.AI,
		Notify:    u.deps.Notify,
		Bootstrap: u.deps.Bootstrap,
	}, steps.PathIntakeInput(in))
}

func (u Usecases) UserProfileRefresh(ctx context.Context, in UserProfileRefreshInput) (UserProfileRefreshOutput, error) {
	return steps.UserProfileRefresh(ctx, steps.UserProfileRefreshDeps{
		DB:          u.deps.DB,
		Log:         u.deps.Log,
		StylePrefs:  u.deps.StylePrefs,
		ProgEvents:  u.deps.ProgEvents,
		UserProfile: u.deps.UserProfile,
		Prefs:       u.deps.Prefs,
		AI:          u.deps.AI,
		Vec:         u.deps.Vec,
		Saga:        u.deps.Saga,
		Bootstrap:   u.deps.Bootstrap,
	}, steps.UserProfileRefreshInput(in))
}

func (u Usecases) TeachingPatternsSeed(ctx context.Context, in TeachingPatternsSeedInput) (TeachingPatternsSeedOutput, error) {
	return steps.TeachingPatternsSeed(ctx, steps.TeachingPatternsSeedDeps{
		DB:          u.deps.DB,
		Log:         u.deps.Log,
		Patterns:    u.deps.TeachingPatterns,
		UserProfile: u.deps.UserProfile,
		AI:          u.deps.AI,
		Vec:         u.deps.Vec,
		Saga:        u.deps.Saga,
		Bootstrap:   u.deps.Bootstrap,
	}, steps.TeachingPatternsSeedInput(in))
}

func (u Usecases) ConceptGraphBuild(ctx context.Context, in ConceptGraphBuildInput) (ConceptGraphBuildOutput, error) {
	return steps.ConceptGraphBuild(ctx, steps.ConceptGraphBuildDeps{
		DB:        u.deps.DB,
		Log:       u.deps.Log,
		Files:     u.deps.Files,
		Chunks:    u.deps.Chunks,
		Path:      u.deps.Path,
		Concepts:  u.deps.Concepts,
		Evidence:  u.deps.Evidence,
		Edges:     u.deps.Edges,
		Graph:     u.deps.Graph,
		AI:        u.deps.AI,
		Vec:       u.deps.Vec,
		Saga:      u.deps.Saga,
		Bootstrap: u.deps.Bootstrap,
	}, steps.ConceptGraphBuildInput(in))
}

func (u Usecases) MaterialKGBuild(ctx context.Context, in MaterialKGBuildInput) (MaterialKGBuildOutput, error) {
	return steps.MaterialKGBuild(ctx, steps.MaterialKGBuildDeps{
		DB:        u.deps.DB,
		Log:       u.deps.Log,
		Files:     u.deps.Files,
		Chunks:    u.deps.Chunks,
		Path:      u.deps.Path,
		Concepts:  u.deps.Concepts,
		Graph:     u.deps.Graph,
		AI:        u.deps.AI,
		Bootstrap: u.deps.Bootstrap,
	}, steps.MaterialKGBuildInput(in))
}

func (u Usecases) ConceptClusterBuild(ctx context.Context, in ConceptClusterBuildInput) (ConceptClusterBuildOutput, error) {
	return steps.ConceptClusterBuild(ctx, steps.ConceptClusterBuildDeps{
		DB:        u.deps.DB,
		Log:       u.deps.Log,
		Concepts:  u.deps.Concepts,
		Clusters:  u.deps.Clusters,
		Members:   u.deps.Members,
		AI:        u.deps.AI,
		Vec:       u.deps.Vec,
		Saga:      u.deps.Saga,
		Bootstrap: u.deps.Bootstrap,
	}, steps.ConceptClusterBuildInput(in))
}

func (u Usecases) ChainSignatureBuild(ctx context.Context, in ChainSignatureBuildInput) (ChainSignatureBuildOutput, error) {
	return steps.ChainSignatureBuild(ctx, steps.ChainSignatureBuildDeps{
		DB:        u.deps.DB,
		Log:       u.deps.Log,
		Concepts:  u.deps.Concepts,
		Clusters:  u.deps.Clusters,
		Members:   u.deps.Members,
		Edges:     u.deps.Edges,
		Chains:    u.deps.ChainSignatures,
		AI:        u.deps.AI,
		Vec:       u.deps.Vec,
		Saga:      u.deps.Saga,
		Bootstrap: u.deps.Bootstrap,
	}, steps.ChainSignatureBuildInput(in))
}

func (u Usecases) PathPlanBuild(ctx context.Context, in PathPlanBuildInput) (PathPlanBuildOutput, error) {
	return steps.PathPlanBuild(ctx, steps.PathPlanBuildDeps{
		DB:           u.deps.DB,
		Log:          u.deps.Log,
		Path:         u.deps.Path,
		PathNodes:    u.deps.PathNodes,
		Concepts:     u.deps.Concepts,
		Edges:        u.deps.Edges,
		Summaries:    u.deps.Summaries,
		UserProfile:  u.deps.UserProfile,
		ConceptState: u.deps.ConceptState,
		Graph:        u.deps.Graph,
		AI:           u.deps.AI,
		Bootstrap:    u.deps.Bootstrap,
	}, steps.PathPlanBuildInput(in))
}

func (u Usecases) PathCoverRender(ctx context.Context, in PathCoverRenderInput) (PathCoverRenderOutput, error) {
	return steps.PathCoverRender(ctx, steps.PathCoverRenderDeps{
		Log:       u.deps.Log,
		Path:      u.deps.Path,
		PathNodes: u.deps.PathNodes,
		Avatar:    u.deps.Avatar,
	}, steps.PathCoverRenderInput(in))
}

func (u Usecases) NodeAvatarRender(ctx context.Context, in NodeAvatarRenderInput) (NodeAvatarRenderOutput, error) {
	return steps.NodeAvatarRender(ctx, steps.NodeAvatarRenderDeps{
		Log:       u.deps.Log,
		Path:      u.deps.Path,
		PathNodes: u.deps.PathNodes,
		Avatar:    u.deps.Avatar,
	}, steps.NodeAvatarRenderInput(in))
}

func (u Usecases) NodeFiguresPlanBuild(ctx context.Context, in NodeFiguresPlanBuildInput) (NodeFiguresPlanBuildOutput, error) {
	return steps.NodeFiguresPlanBuild(ctx, steps.NodeFiguresPlanBuildDeps{
		DB:        u.deps.DB,
		Log:       u.deps.Log,
		Path:      u.deps.Path,
		PathNodes: u.deps.PathNodes,
		Figures:   u.deps.Figures,
		GenRuns:   u.deps.GenRuns,
		Files:     u.deps.Files,
		Chunks:    u.deps.Chunks,
		AI:        u.deps.AI,
		Vec:       u.deps.Vec,
		Bootstrap: u.deps.Bootstrap,
	}, steps.NodeFiguresPlanBuildInput(in))
}

func (u Usecases) NodeFiguresRender(ctx context.Context, in NodeFiguresRenderInput) (NodeFiguresRenderOutput, error) {
	return steps.NodeFiguresRender(ctx, steps.NodeFiguresRenderDeps{
		DB:        u.deps.DB,
		Log:       u.deps.Log,
		Path:      u.deps.Path,
		PathNodes: u.deps.PathNodes,
		Figures:   u.deps.Figures,
		Assets:    u.deps.Assets,
		GenRuns:   u.deps.GenRuns,
		AI:        u.deps.AI,
		Bucket:    u.deps.Bucket,
		Bootstrap: u.deps.Bootstrap,
	}, steps.NodeFiguresRenderInput(in))
}

func (u Usecases) NodeVideosPlanBuild(ctx context.Context, in NodeVideosPlanBuildInput) (NodeVideosPlanBuildOutput, error) {
	return steps.NodeVideosPlanBuild(ctx, steps.NodeVideosPlanBuildDeps{
		DB:        u.deps.DB,
		Log:       u.deps.Log,
		Path:      u.deps.Path,
		PathNodes: u.deps.PathNodes,
		Videos:    u.deps.Videos,
		GenRuns:   u.deps.GenRuns,
		Files:     u.deps.Files,
		Chunks:    u.deps.Chunks,
		AI:        u.deps.AI,
		Vec:       u.deps.Vec,
		Bootstrap: u.deps.Bootstrap,
	}, steps.NodeVideosPlanBuildInput(in))
}

func (u Usecases) NodeVideosRender(ctx context.Context, in NodeVideosRenderInput) (NodeVideosRenderOutput, error) {
	return steps.NodeVideosRender(ctx, steps.NodeVideosRenderDeps{
		DB:        u.deps.DB,
		Log:       u.deps.Log,
		Path:      u.deps.Path,
		PathNodes: u.deps.PathNodes,
		Videos:    u.deps.Videos,
		Assets:    u.deps.Assets,
		GenRuns:   u.deps.GenRuns,
		AI:        u.deps.AI,
		Bucket:    u.deps.Bucket,
		Bootstrap: u.deps.Bootstrap,
	}, steps.NodeVideosRenderInput(in))
}

func (u Usecases) NodeContentBuild(ctx context.Context, in NodeContentBuildInput) (NodeContentBuildOutput, error) {
	return steps.NodeContentBuild(ctx, steps.NodeContentBuildDeps{
		DB:          u.deps.DB,
		Log:         u.deps.Log,
		Path:        u.deps.Path,
		PathNodes:   u.deps.PathNodes,
		Files:       u.deps.Files,
		Chunks:      u.deps.Chunks,
		UserProfile: u.deps.UserProfile,
		Patterns:    u.deps.TeachingPatterns,
		AI:          u.deps.AI,
		Vec:         u.deps.Vec,
		Bucket:      u.deps.Bucket,
		Bootstrap:   u.deps.Bootstrap,
	}, steps.NodeContentBuildInput(in))
}

func (u Usecases) NodeDocBuild(ctx context.Context, in NodeDocBuildInput) (NodeDocBuildOutput, error) {
	return steps.NodeDocBuild(ctx, steps.NodeDocBuildDeps{
		DB:               u.deps.DB,
		Log:              u.deps.Log,
		Path:             u.deps.Path,
		PathNodes:        u.deps.PathNodes,
		NodeDocs:         u.deps.NodeDocs,
		Figures:          u.deps.Figures,
		Videos:           u.deps.Videos,
		GenRuns:          u.deps.GenRuns,
		Files:            u.deps.Files,
		Chunks:           u.deps.Chunks,
		UserProfile:      u.deps.UserProfile,
		TeachingPatterns: u.deps.TeachingPatterns,
		Concepts:         u.deps.Concepts,
		ConceptState:     u.deps.ConceptState,
		AI:               u.deps.AI,
		Vec:              u.deps.Vec,
		Bucket:           u.deps.Bucket,
		Bootstrap:        u.deps.Bootstrap,
	}, steps.NodeDocBuildInput(in))
}

func (u Usecases) NodeDocPatch(ctx context.Context, in NodeDocPatchInput) (NodeDocPatchOutput, error) {
	return steps.NodeDocPatch(ctx, steps.NodeDocPatchDeps{
		DB:        u.deps.DB,
		Log:       u.deps.Log,
		Path:      u.deps.Path,
		PathNodes: u.deps.PathNodes,
		NodeDocs:  u.deps.NodeDocs,
		Figures:   u.deps.Figures,
		Videos:    u.deps.Videos,
		Revisions: u.deps.Revisions,
		Files:     u.deps.Files,
		Chunks:    u.deps.Chunks,
		ULI:       u.deps.ULI,
		Assets:    u.deps.Assets,
		AI:        u.deps.AI,
		Vec:       u.deps.Vec,
		Bucket:    u.deps.Bucket,
	}, steps.NodeDocPatchInput(in))
}

func (u Usecases) RealizeActivities(ctx context.Context, in RealizeActivitiesInput) (RealizeActivitiesOutput, error) {
	return steps.RealizeActivities(ctx, steps.RealizeActivitiesDeps{
		DB:                 u.deps.DB,
		Log:                u.deps.Log,
		Path:               u.deps.Path,
		PathNodes:          u.deps.PathNodes,
		PathNodeActivities: u.deps.PathNodeActivities,
		Activities:         u.deps.Activities,
		Variants:           u.deps.Variants,
		ActivityConcepts:   u.deps.ActivityConcepts,
		ActivityCitations:  u.deps.ActivityCitations,
		Concepts:           u.deps.Concepts,
		ConceptState:       u.deps.ConceptState,
		Files:              u.deps.Files,
		Chunks:             u.deps.Chunks,
		UserProfile:        u.deps.UserProfile,
		Patterns:           u.deps.TeachingPatterns,
		Graph:              u.deps.Graph,
		AI:                 u.deps.AI,
		Vec:                u.deps.Vec,
		Saga:               u.deps.Saga,
		Bootstrap:          u.deps.Bootstrap,
	}, steps.RealizeActivitiesInput(in))
}

func (u Usecases) CoverageCoherenceAudit(ctx context.Context, in CoverageCoherenceAuditInput) (CoverageCoherenceAuditOutput, error) {
	return steps.CoverageCoherenceAudit(ctx, steps.CoverageCoherenceAuditDeps{
		DB:         u.deps.DB,
		Log:        u.deps.Log,
		Path:       u.deps.Path,
		PathNodes:  u.deps.PathNodes,
		Concepts:   u.deps.Concepts,
		Activities: u.deps.Activities,
		Variants:   u.deps.Variants,
		AI:         u.deps.AI,
		Bootstrap:  u.deps.Bootstrap,
	}, steps.CoverageCoherenceAuditInput(in))
}

func (u Usecases) ProgressionCompact(ctx context.Context, in ProgressionCompactInput) (ProgressionCompactOutput, error) {
	return steps.ProgressionCompact(ctx, steps.ProgressionCompactDeps{
		DB:        u.deps.DB,
		Log:       u.deps.Log,
		Events:    u.deps.UserEvents,
		Cursors:   u.deps.UserEventCursors,
		Progress:  u.deps.ProgEvents,
		Bootstrap: u.deps.Bootstrap,
	}, steps.ProgressionCompactInput(in))
}

func (u Usecases) VariantStatsRefresh(ctx context.Context, in VariantStatsRefreshInput) (VariantStatsRefreshOutput, error) {
	return steps.VariantStatsRefresh(ctx, steps.VariantStatsRefreshDeps{
		DB:        u.deps.DB,
		Log:       u.deps.Log,
		Events:    u.deps.UserEvents,
		Cursors:   u.deps.UserEventCursors,
		Variants:  u.deps.Variants,
		Stats:     u.deps.VariantStats,
		Bootstrap: u.deps.Bootstrap,
	}, steps.VariantStatsRefreshInput(in))
}

func (u Usecases) PriorsRefresh(ctx context.Context, in PriorsRefreshInput) (PriorsRefreshOutput, error) {
	return steps.PriorsRefresh(ctx, steps.PriorsRefreshDeps{
		DB:           u.deps.DB,
		Log:          u.deps.Log,
		Activities:   u.deps.Activities,
		Variants:     u.deps.Variants,
		VariantStats: u.deps.VariantStats,
		Chains:       u.deps.ChainSignatures,
		Concepts:     u.deps.Concepts,
		ActConcepts:  u.deps.ActivityConcepts,
		ChainPriors:  u.deps.ChainPriors,
		CohortPriors: u.deps.CohortPriors,
		Bootstrap:    u.deps.Bootstrap,
	}, steps.PriorsRefreshInput(in))
}

func (u Usecases) CompletedUnitRefresh(ctx context.Context, in CompletedUnitRefreshInput) (CompletedUnitRefreshOutput, error) {
	return steps.CompletedUnitRefresh(ctx, steps.CompletedUnitRefreshDeps{
		DB:        u.deps.DB,
		Log:       u.deps.Log,
		Completed: u.deps.CompletedUnits,
		Progress:  u.deps.ProgEvents,
		Concepts:  u.deps.Concepts,
		Act:       u.deps.Activities,
		ActCon:    u.deps.ActivityConcepts,
		Chains:    u.deps.ChainSignatures,
		Mastery:   u.deps.ConceptState,
		Graph:     u.deps.Graph,
		Bootstrap: u.deps.Bootstrap,
	}, steps.CompletedUnitRefreshInput(in))
}

func (u Usecases) SagaCleanup(ctx context.Context, in SagaCleanupInput) (SagaCleanupOutput, error) {
	return steps.SagaCleanup(ctx, steps.SagaCleanupDeps{
		DB:      u.deps.DB,
		Log:     u.deps.Log,
		Sagas:   u.deps.Sagas,
		SagaSvc: u.deps.Saga,
		Bucket:  u.deps.Bucket,
	}, steps.SagaCleanupInput(in))
}
