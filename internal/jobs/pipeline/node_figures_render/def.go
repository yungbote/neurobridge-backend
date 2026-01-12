package node_figures_render

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/platform/gcp"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type Pipeline struct {
	db        *gorm.DB
	log       *logger.Logger
	path      repos.PathRepo
	nodes     repos.PathNodeRepo
	figures   repos.LearningNodeFigureRepo
	assets    repos.AssetRepo
	genRuns   repos.LearningDocGenerationRunRepo
	ai        openai.Client
	bucket    gcp.BucketService
	bootstrap services.LearningBuildBootstrapService
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	path repos.PathRepo,
	nodes repos.PathNodeRepo,
	figures repos.LearningNodeFigureRepo,
	assets repos.AssetRepo,
	genRuns repos.LearningDocGenerationRunRepo,
	ai openai.Client,
	bucket gcp.BucketService,
	bootstrap services.LearningBuildBootstrapService,
) *Pipeline {
	return &Pipeline{
		db:        db,
		log:       baseLog.With("job", "node_figures_render"),
		path:      path,
		nodes:     nodes,
		figures:   figures,
		assets:    assets,
		genRuns:   genRuns,
		ai:        ai,
		bucket:    bucket,
		bootstrap: bootstrap,
	}
}

func (p *Pipeline) Type() string { return "node_figures_render" }
