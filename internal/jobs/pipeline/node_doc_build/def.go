package node_doc_build

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
	db        *gorm.DB
	log       *logger.Logger
	path      repos.PathRepo
	nodes     repos.PathNodeRepo
	docs      repos.LearningNodeDocRepo
	figures   repos.LearningNodeFigureRepo
	videos    repos.LearningNodeVideoRepo
	genRuns   repos.LearningDocGenerationRunRepo
	files     repos.MaterialFileRepo
	chunks    repos.MaterialChunkRepo
	userProf  repos.UserProfileVectorRepo
	patterns  repos.TeachingPatternRepo
	concepts  repos.ConceptRepo
	mastery   repos.UserConceptStateRepo
	model     repos.UserConceptModelRepo
	miscon    repos.UserMisconceptionInstanceRepo
	ai        openai.Client
	vec       pinecone.VectorStore
	bucket    gcp.BucketService
	bootstrap services.LearningBuildBootstrapService
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	path repos.PathRepo,
	nodes repos.PathNodeRepo,
	docs repos.LearningNodeDocRepo,
	figures repos.LearningNodeFigureRepo,
	videos repos.LearningNodeVideoRepo,
	genRuns repos.LearningDocGenerationRunRepo,
	files repos.MaterialFileRepo,
	chunks repos.MaterialChunkRepo,
	userProf repos.UserProfileVectorRepo,
	patterns repos.TeachingPatternRepo,
	concepts repos.ConceptRepo,
	mastery repos.UserConceptStateRepo,
	model repos.UserConceptModelRepo,
	miscon repos.UserMisconceptionInstanceRepo,
	ai openai.Client,
	vec pinecone.VectorStore,
	bucket gcp.BucketService,
	bootstrap services.LearningBuildBootstrapService,
) *Pipeline {
	return &Pipeline{
		db:        db,
		log:       baseLog.With("job", "node_doc_build"),
		path:      path,
		nodes:     nodes,
		docs:      docs,
		figures:   figures,
		videos:    videos,
		genRuns:   genRuns,
		files:     files,
		chunks:    chunks,
		userProf:  userProf,
		patterns:  patterns,
		concepts:  concepts,
		mastery:   mastery,
		model:     model,
		miscon:    miscon,
		ai:        ai,
		vec:       vec,
		bucket:    bucket,
		bootstrap: bootstrap,
	}
}

func (p *Pipeline) Type() string { return "node_doc_build" }
