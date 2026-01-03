package path_cover_render

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/clients/gcp"
	"github.com/yungbote/neurobridge-backend/internal/clients/openai"
	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type Pipeline struct {
	db        *gorm.DB
	log       *logger.Logger
	path      repos.PathRepo
	nodes     repos.PathNodeRepo
	assets    repos.AssetRepo
	ai        openai.Client
	bucket    gcp.BucketService
	bootstrap services.LearningBuildBootstrapService
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	path repos.PathRepo,
	nodes repos.PathNodeRepo,
	assets repos.AssetRepo,
	ai openai.Client,
	bucket gcp.BucketService,
	bootstrap services.LearningBuildBootstrapService,
) *Pipeline {
	return &Pipeline{
		db:        db,
		log:       baseLog.With("job", "path_cover_render"),
		path:      path,
		nodes:     nodes,
		assets:    assets,
		ai:        ai,
		bucket:    bucket,
		bootstrap: bootstrap,
	}
}

func (p *Pipeline) Type() string { return "path_cover_render" }
