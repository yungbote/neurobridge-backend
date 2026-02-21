package chat

import (
	"context"

	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	domainagg "github.com/yungbote/neurobridge-backend/internal/domain/aggregates"
	"github.com/yungbote/neurobridge-backend/internal/modules/chat/steps"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/neo4jdb"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
	"github.com/yungbote/neurobridge-backend/internal/platform/pinecone"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type UsecasesDeps struct {
	DB  *gorm.DB
	Log *logger.Logger

	AI    openai.Client
	Vec   pinecone.VectorStore
	Graph *neo4jdb.Client

	Threads   repos.ChatThreadRepo
	Messages  repos.ChatMessageRepo
	State     repos.ChatThreadStateRepo
	Summaries repos.ChatSummaryNodeRepo
	ThreadAgg domainagg.ThreadAggregate

	Docs     repos.ChatDocRepo
	Turns    repos.ChatTurnRepo
	Memory   repos.ChatMemoryItemRepo
	Entities repos.ChatEntityRepo
	Edges    repos.ChatEdgeRepo
	Claims   repos.ChatClaimRepo

	JobRuns repos.JobRunRepo
	Jobs    services.JobService
	Notify  services.ChatNotifier

	// Optional: path docs projection used for hybrid retrieval in chat threads.
	Path         repos.PathRepo
	PathNodes    repos.PathNodeRepo
	NodeActs     repos.PathNodeActivityRepo
	Activities   repos.ActivityRepo
	Concepts     repos.ConceptRepo
	NodeDocs     repos.LearningNodeDocRepo
	ConceptEdges repos.ConceptEdgeRepo
	ConceptState repos.UserConceptStateRepo
	ConceptModel repos.UserConceptModelRepo
	Sessions     repos.UserSessionStateRepo
	MisconRepo   repos.UserMisconceptionInstanceRepo

	UserLibraryIndex     repos.UserLibraryIndexRepo
	MaterialFiles        repos.MaterialFileRepo
	MaterialSetSummaries repos.MaterialSetSummaryRepo
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
	RespondInput  = steps.RespondInput
	RespondOutput = steps.RespondOutput

	MaintainInput = steps.MaintainInput

	PathIndexInput  = steps.PathIndexInput
	PathIndexOutput = steps.PathIndexOutput

	PathNodeIndexInput  = steps.PathNodeIndexInput
	PathNodeIndexOutput = steps.PathNodeIndexOutput

	RebuildInput = steps.RebuildInput
)

func (u Usecases) Respond(ctx context.Context, in RespondInput) (RespondOutput, error) {
	return steps.Respond(ctx, steps.RespondDeps{
		DB:        u.deps.DB,
		Log:       u.deps.Log,
		AI:        u.deps.AI,
		Vec:       u.deps.Vec,
		Threads:   u.deps.Threads,
		Messages:  u.deps.Messages,
		State:     u.deps.State,
		Summaries: u.deps.Summaries,
		Docs:      u.deps.Docs,
		Turns:     u.deps.Turns,
		ThreadAgg: u.deps.ThreadAgg,
		Path:      u.deps.Path,
		PathNodes: u.deps.PathNodes,
		NodeDocs:  u.deps.NodeDocs,
		Concepts:  u.deps.Concepts,
		Edges:     u.deps.ConceptEdges,
		Mastery:   u.deps.ConceptState,
		Models:    u.deps.ConceptModel,
		Miscon:    u.deps.MisconRepo,
		Sessions:  u.deps.Sessions,
		JobRuns:   u.deps.JobRuns,
		Jobs:      u.deps.Jobs,
		Notify:    u.deps.Notify,
	}, steps.RespondInput(in))
}

func (u Usecases) MaintainThread(ctx context.Context, in MaintainInput) error {
	return steps.MaintainThread(ctx, steps.MaintainDeps{
		DB:        u.deps.DB,
		Log:       u.deps.Log,
		AI:        u.deps.AI,
		Vec:       u.deps.Vec,
		Graph:     u.deps.Graph,
		Threads:   u.deps.Threads,
		Messages:  u.deps.Messages,
		State:     u.deps.State,
		Summaries: u.deps.Summaries,
		Docs:      u.deps.Docs,
		Memory:    u.deps.Memory,
		Entities:  u.deps.Entities,
		Edges:     u.deps.Edges,
		Claims:    u.deps.Claims,
	}, steps.MaintainInput(in))
}

func (u Usecases) IndexPathDocsForChat(ctx context.Context, in PathIndexInput) (PathIndexOutput, error) {
	return steps.IndexPathDocsForChat(ctx, steps.PathIndexDeps{
		DB:                   u.deps.DB,
		Log:                  u.deps.Log,
		AI:                   u.deps.AI,
		Vec:                  u.deps.Vec,
		Docs:                 u.deps.Docs,
		Path:                 u.deps.Path,
		PathNodes:            u.deps.PathNodes,
		NodeActs:             u.deps.NodeActs,
		Activities:           u.deps.Activities,
		Concepts:             u.deps.Concepts,
		NodeDocs:             u.deps.NodeDocs,
		UserLibraryIndex:     u.deps.UserLibraryIndex,
		MaterialFiles:        u.deps.MaterialFiles,
		MaterialSetSummaries: u.deps.MaterialSetSummaries,
	}, steps.PathIndexInput(in))
}

func (u Usecases) IndexPathNodeBlocksForChat(ctx context.Context, in PathNodeIndexInput) (PathNodeIndexOutput, error) {
	return steps.IndexPathNodeBlocksForChat(ctx, steps.PathIndexDeps{
		DB:                   u.deps.DB,
		Log:                  u.deps.Log,
		AI:                   u.deps.AI,
		Vec:                  u.deps.Vec,
		Docs:                 u.deps.Docs,
		Path:                 u.deps.Path,
		PathNodes:            u.deps.PathNodes,
		NodeActs:             u.deps.NodeActs,
		Activities:           u.deps.Activities,
		Concepts:             u.deps.Concepts,
		NodeDocs:             u.deps.NodeDocs,
		UserLibraryIndex:     u.deps.UserLibraryIndex,
		MaterialFiles:        u.deps.MaterialFiles,
		MaterialSetSummaries: u.deps.MaterialSetSummaries,
	}, steps.PathNodeIndexInput(in))
}

func (u Usecases) RebuildThreadProjections(ctx context.Context, in RebuildInput) error {
	return steps.RebuildThreadProjections(ctx, steps.RebuildDeps{
		DB:  u.deps.DB,
		Log: u.deps.Log,
		Vec: u.deps.Vec,
	}, steps.RebuildInput(in))
}

func (u Usecases) PurgeThreadArtifacts(ctx context.Context, in RebuildInput) error {
	return steps.PurgeThreadArtifacts(ctx, steps.RebuildDeps{
		DB:  u.deps.DB,
		Log: u.deps.Log,
		Vec: u.deps.Vec,
	}, steps.RebuildInput(in))
}
