package chat_path_node_index

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
	"github.com/yungbote/neurobridge-backend/internal/platform/pinecone"
)

type Pipeline struct {
	db  *gorm.DB
	log *logger.Logger

	ai  openai.Client
	vec pinecone.VectorStore

	docs repos.ChatDocRepo

	path      repos.PathRepo
	pathNodes repos.PathNodeRepo
	nodeDocs  repos.LearningNodeDocRepo
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	ai openai.Client,
	vec pinecone.VectorStore,
	docs repos.ChatDocRepo,
	path repos.PathRepo,
	pathNodes repos.PathNodeRepo,
	nodeDocs repos.LearningNodeDocRepo,
) *Pipeline {
	return &Pipeline{
		db:        db,
		log:       baseLog.With("job", "chat_path_node_index"),
		ai:        ai,
		vec:       vec,
		docs:      docs,
		path:      path,
		pathNodes: pathNodes,
		nodeDocs:  nodeDocs,
	}
}

func (p *Pipeline) Type() string { return "chat_path_node_index" }
