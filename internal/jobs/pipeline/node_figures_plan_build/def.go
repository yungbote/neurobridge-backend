package node_figures_plan_build

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
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
	figures   repos.LearningNodeFigureRepo
	genRuns   repos.LearningDocGenerationRunRepo
	files     repos.MaterialFileRepo
	chunks    repos.MaterialChunkRepo
	ai        openai.Client
	vec       pinecone.VectorStore
	bootstrap services.LearningBuildBootstrapService
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	path repos.PathRepo,
	nodes repos.PathNodeRepo,
	figures repos.LearningNodeFigureRepo,
	genRuns repos.LearningDocGenerationRunRepo,
	files repos.MaterialFileRepo,
	chunks repos.MaterialChunkRepo,
	ai openai.Client,
	vec pinecone.VectorStore,
	bootstrap services.LearningBuildBootstrapService,
) *Pipeline {
	return &Pipeline{
		db:        db,
		log:       baseLog.With("job", "node_figures_plan_build"),
		path:      path,
		nodes:     nodes,
		figures:   figures,
		genRuns:   genRuns,
		files:     files,
		chunks:    chunks,
		ai:        ai,
		vec:       vec,
		bootstrap: bootstrap,
	}
}

func (p *Pipeline) Type() string { return "node_figures_plan_build" }
