package material_kg_build

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/neo4jdb"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type Pipeline struct {
	db        *gorm.DB
	log       *logger.Logger
	files     repos.MaterialFileRepo
	chunks    repos.MaterialChunkRepo
	path      repos.PathRepo
	concepts  repos.ConceptRepo
	graph     *neo4jdb.Client
	ai        openai.Client
	saga      services.SagaService
	bootstrap services.LearningBuildBootstrapService
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	files repos.MaterialFileRepo,
	chunks repos.MaterialChunkRepo,
	path repos.PathRepo,
	concepts repos.ConceptRepo,
	graph *neo4jdb.Client,
	ai openai.Client,
	saga services.SagaService,
	bootstrap services.LearningBuildBootstrapService,
) *Pipeline {
	return &Pipeline{
		db:        db,
		log:       baseLog.With("job", "material_kg_build"),
		files:     files,
		chunks:    chunks,
		path:      path,
		concepts:  concepts,
		graph:     graph,
		ai:        ai,
		saga:      saga,
		bootstrap: bootstrap,
	}
}

func (p *Pipeline) Type() string { return "material_kg_build" }
