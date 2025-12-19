package concept_graph_build

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/clients/openai"
	"github.com/yungbote/neurobridge-backend/internal/clients/pinecone"
	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type Pipeline struct {
	db        *gorm.DB
	log       *logger.Logger
	files     repos.MaterialFileRepo
	chunks    repos.MaterialChunkRepo
	concepts  repos.ConceptRepo
	evidence  repos.ConceptEvidenceRepo
	edges     repos.ConceptEdgeRepo
	ai        openai.Client
	vec       pinecone.VectorStore
	saga      services.SagaService
	bootstrap services.LearningBuildBootstrapService
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	files repos.MaterialFileRepo,
	chunks repos.MaterialChunkRepo,
	concepts repos.ConceptRepo,
	evidence repos.ConceptEvidenceRepo,
	edges repos.ConceptEdgeRepo,
	ai openai.Client,
	vec pinecone.VectorStore,
	saga services.SagaService,
	bootstrap services.LearningBuildBootstrapService,
) *Pipeline {
	return &Pipeline{
		db:        db,
		log:       baseLog.With("job", "concept_graph_build"),
		files:     files,
		chunks:    chunks,
		concepts:  concepts,
		evidence:  evidence,
		edges:     edges,
		ai:        ai,
		vec:       vec,
		saga:      saga,
		bootstrap: bootstrap,
	}
}

func (p *Pipeline) Type() string { return "concept_graph_build" }
