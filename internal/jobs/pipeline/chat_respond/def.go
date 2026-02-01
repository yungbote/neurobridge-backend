package chat_respond

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
	"github.com/yungbote/neurobridge-backend/internal/platform/pinecone"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type Pipeline struct {
	db  *gorm.DB
	log *logger.Logger

	ai  openai.Client
	vec pinecone.VectorStore

	threads   repos.ChatThreadRepo
	messages  repos.ChatMessageRepo
	state     repos.ChatThreadStateRepo
	summaries repos.ChatSummaryNodeRepo
	docs      repos.ChatDocRepo
	turns     repos.ChatTurnRepo
	path      repos.PathRepo
	pathNodes repos.PathNodeRepo
	nodeDocs  repos.LearningNodeDocRepo
	concepts  repos.ConceptRepo
	edges     repos.ConceptEdgeRepo
	mastery   repos.UserConceptStateRepo
	models    repos.UserConceptModelRepo
	miscon    repos.UserMisconceptionInstanceRepo
	sessions  repos.UserSessionStateRepo

	jobRuns repos.JobRunRepo
	jobs    services.JobService
	notify  services.ChatNotifier
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	ai openai.Client,
	vec pinecone.VectorStore,
	threads repos.ChatThreadRepo,
	messages repos.ChatMessageRepo,
	state repos.ChatThreadStateRepo,
	summaries repos.ChatSummaryNodeRepo,
	docs repos.ChatDocRepo,
	turns repos.ChatTurnRepo,
	path repos.PathRepo,
	pathNodes repos.PathNodeRepo,
	nodeDocs repos.LearningNodeDocRepo,
	concepts repos.ConceptRepo,
	edges repos.ConceptEdgeRepo,
	mastery repos.UserConceptStateRepo,
	models repos.UserConceptModelRepo,
	miscon repos.UserMisconceptionInstanceRepo,
	sessions repos.UserSessionStateRepo,
	jobRuns repos.JobRunRepo,
	jobs services.JobService,
	notify services.ChatNotifier,
) *Pipeline {
	return &Pipeline{
		db:        db,
		log:       baseLog.With("job", "chat_respond"),
		ai:        ai,
		vec:       vec,
		threads:   threads,
		messages:  messages,
		state:     state,
		summaries: summaries,
		docs:      docs,
		turns:     turns,
		path:      path,
		pathNodes: pathNodes,
		nodeDocs:  nodeDocs,
		concepts:  concepts,
		edges:     edges,
		mastery:   mastery,
		models:    models,
		miscon:    miscon,
		sessions:  sessions,
		jobRuns:   jobRuns,
		jobs:      jobs,
		notify:    notify,
	}
}

func (p *Pipeline) Type() string { return "chat_respond" }
