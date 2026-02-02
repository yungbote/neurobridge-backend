package node_doc_edit

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/platform/gcp"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
	"github.com/yungbote/neurobridge-backend/internal/platform/pinecone"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type Pipeline struct {
	db       *gorm.DB
	log      *logger.Logger
	threads  repos.ChatThreadRepo
	messages repos.ChatMessageRepo
	path     repos.PathRepo
	nodes    repos.PathNodeRepo
	docs     repos.LearningNodeDocRepo
	figures  repos.LearningNodeFigureRepo
	videos   repos.LearningNodeVideoRepo
	files    repos.MaterialFileRepo
	chunks   repos.MaterialChunkRepo
	uli      repos.UserLibraryIndexRepo
	assets   repos.AssetRepo
	ai       openai.Client
	vec      pinecone.VectorStore
	bucket   gcp.BucketService
	notify   services.ChatNotifier
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	threads repos.ChatThreadRepo,
	messages repos.ChatMessageRepo,
	path repos.PathRepo,
	nodes repos.PathNodeRepo,
	docs repos.LearningNodeDocRepo,
	figures repos.LearningNodeFigureRepo,
	videos repos.LearningNodeVideoRepo,
	files repos.MaterialFileRepo,
	chunks repos.MaterialChunkRepo,
	uli repos.UserLibraryIndexRepo,
	assets repos.AssetRepo,
	ai openai.Client,
	vec pinecone.VectorStore,
	bucket gcp.BucketService,
	notify services.ChatNotifier,
) *Pipeline {
	return &Pipeline{
		db:       db,
		log:      baseLog.With("job", "node_doc_edit"),
		threads:  threads,
		messages: messages,
		path:     path,
		nodes:    nodes,
		docs:     docs,
		figures:  figures,
		videos:   videos,
		files:    files,
		chunks:   chunks,
		uli:      uli,
		assets:   assets,
		ai:       ai,
		vec:      vec,
		bucket:   bucket,
		notify:   notify,
	}
}

func (p *Pipeline) Type() string { return "node_doc_edit" }
