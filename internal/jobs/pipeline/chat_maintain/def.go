package chat_maintain

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/neo4jdb"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
	"github.com/yungbote/neurobridge-backend/internal/platform/pinecone"
)

type Pipeline struct {
	db  *gorm.DB
	log *logger.Logger

	ai    openai.Client
	vec   pinecone.VectorStore
	graph *neo4jdb.Client

	threads   repos.ChatThreadRepo
	messages  repos.ChatMessageRepo
	state     repos.ChatThreadStateRepo
	summaries repos.ChatSummaryNodeRepo

	docs     repos.ChatDocRepo
	memory   repos.ChatMemoryItemRepo
	entities repos.ChatEntityRepo
	edges    repos.ChatEdgeRepo
	claims   repos.ChatClaimRepo
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	ai openai.Client,
	vec pinecone.VectorStore,
	graph *neo4jdb.Client,
	threads repos.ChatThreadRepo,
	messages repos.ChatMessageRepo,
	state repos.ChatThreadStateRepo,
	summaries repos.ChatSummaryNodeRepo,
	docs repos.ChatDocRepo,
	memory repos.ChatMemoryItemRepo,
	entities repos.ChatEntityRepo,
	edges repos.ChatEdgeRepo,
	claims repos.ChatClaimRepo,
) *Pipeline {
	return &Pipeline{
		db:        db,
		log:       baseLog.With("job", "chat_maintain"),
		ai:        ai,
		vec:       vec,
		graph:     graph,
		threads:   threads,
		messages:  messages,
		state:     state,
		summaries: summaries,
		docs:      docs,
		memory:    memory,
		entities:  entities,
		edges:     edges,
		claims:    claims,
	}
}

func (p *Pipeline) Type() string { return "chat_maintain" }
