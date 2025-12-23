package chat_path_index

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/clients/openai"
	"github.com/yungbote/neurobridge-backend/internal/clients/pinecone"
	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type Pipeline struct {
	db  *gorm.DB
	log *logger.Logger

	ai  openai.Client
	vec pinecone.VectorStore

	docs repos.ChatDocRepo

	path       repos.PathRepo
	pathNodes  repos.PathNodeRepo
	nodeActs   repos.PathNodeActivityRepo
	activities repos.ActivityRepo
	concepts   repos.ConceptRepo
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	ai openai.Client,
	vec pinecone.VectorStore,
	docs repos.ChatDocRepo,
	path repos.PathRepo,
	pathNodes repos.PathNodeRepo,
	nodeActs repos.PathNodeActivityRepo,
	activities repos.ActivityRepo,
	concepts repos.ConceptRepo,
) *Pipeline {
	return &Pipeline{
		db:         db,
		log:        baseLog.With("job", "chat_path_index"),
		ai:         ai,
		vec:        vec,
		docs:       docs,
		path:       path,
		pathNodes:  pathNodes,
		nodeActs:   nodeActs,
		activities: activities,
		concepts:   concepts,
	}
}

func (p *Pipeline) Type() string { return "chat_path_index" }
