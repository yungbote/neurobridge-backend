package concept_bridge_build

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
	concepts  repos.ConceptRepo
	edges     repos.ConceptEdgeRepo
	ai        openai.Client
	vec       pinecone.VectorStore
	bootstrap services.LearningBuildBootstrapService
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	concepts repos.ConceptRepo,
	edges repos.ConceptEdgeRepo,
	ai openai.Client,
	vec pinecone.VectorStore,
	bootstrap services.LearningBuildBootstrapService,
) *Pipeline {
	return &Pipeline{
		db:        db,
		log:       baseLog.With("job", "concept_bridge_build"),
		concepts:  concepts,
		edges:     edges,
		ai:        ai,
		vec:       vec,
		bootstrap: bootstrap,
	}
}

func (p *Pipeline) Type() string { return "concept_bridge_build" }

