package node_doc_patch

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/platform/gcp"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
	"github.com/yungbote/neurobridge-backend/internal/platform/pinecone"
)

type Pipeline struct {
	db        *gorm.DB
	log       *logger.Logger
	path      repos.PathRepo
	nodes     repos.PathNodeRepo
	docs      repos.LearningNodeDocRepo
	figures   repos.LearningNodeFigureRepo
	videos    repos.LearningNodeVideoRepo
	revisions repos.LearningNodeDocRevisionRepo
	files     repos.MaterialFileRepo
	chunks    repos.MaterialChunkRepo
	uli       repos.UserLibraryIndexRepo
	assets    repos.AssetRepo
	ai        openai.Client
	vec       pinecone.VectorStore
	bucket    gcp.BucketService
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	path repos.PathRepo,
	nodes repos.PathNodeRepo,
	docs repos.LearningNodeDocRepo,
	figures repos.LearningNodeFigureRepo,
	videos repos.LearningNodeVideoRepo,
	revisions repos.LearningNodeDocRevisionRepo,
	files repos.MaterialFileRepo,
	chunks repos.MaterialChunkRepo,
	uli repos.UserLibraryIndexRepo,
	assets repos.AssetRepo,
	ai openai.Client,
	vec pinecone.VectorStore,
	bucket gcp.BucketService,
) *Pipeline {
	return &Pipeline{
		db:        db,
		log:       baseLog.With("job", "node_doc_patch"),
		path:      path,
		nodes:     nodes,
		docs:      docs,
		figures:   figures,
		videos:    videos,
		revisions: revisions,
		files:     files,
		chunks:    chunks,
		uli:       uli,
		assets:    assets,
		ai:        ai,
		vec:       vec,
		bucket:    bucket,
	}
}

func (p *Pipeline) Type() string { return "node_doc_patch" }
