package chat_path_index

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

	path       repos.PathRepo
	pathNodes  repos.PathNodeRepo
	nodeActs   repos.PathNodeActivityRepo
	activities repos.ActivityRepo
	concepts   repos.ConceptRepo
	nodeDocs   repos.LearningNodeDocRepo

	userLibraryIndex     repos.UserLibraryIndexRepo
	materialFiles        repos.MaterialFileRepo
	materialSetSummaries repos.MaterialSetSummaryRepo
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
	nodeDocs repos.LearningNodeDocRepo,
	userLibraryIndex repos.UserLibraryIndexRepo,
	materialFiles repos.MaterialFileRepo,
	materialSetSummaries repos.MaterialSetSummaryRepo,
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
		nodeDocs:   nodeDocs,

		userLibraryIndex:     userLibraryIndex,
		materialFiles:        materialFiles,
		materialSetSummaries: materialSetSummaries,
	}
}

func (p *Pipeline) Type() string { return "chat_path_index" }
