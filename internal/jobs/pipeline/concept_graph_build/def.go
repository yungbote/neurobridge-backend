package concept_graph_build

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/neo4jdb"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
	"github.com/yungbote/neurobridge-backend/internal/platform/pinecone"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type Pipeline struct {
	db        *gorm.DB
	log       *logger.Logger
	files     repos.MaterialFileRepo
	fileSigs  repos.MaterialFileSignatureRepo
	chunks    repos.MaterialChunkRepo
	path      repos.PathRepo
	concepts  repos.ConceptRepo
	reps      repos.ConceptRepresentationRepo
	overrides repos.ConceptMappingOverrideRepo
	evidence  repos.ConceptEvidenceRepo
	edges     repos.ConceptEdgeRepo
	graph     *neo4jdb.Client
	ai        openai.Client
	vec       pinecone.VectorStore
	saga      services.SagaService
	bootstrap services.LearningBuildBootstrapService
	artifacts repos.LearningArtifactRepo
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	files repos.MaterialFileRepo,
	fileSigs repos.MaterialFileSignatureRepo,
	chunks repos.MaterialChunkRepo,
	path repos.PathRepo,
	concepts repos.ConceptRepo,
	reps repos.ConceptRepresentationRepo,
	overrides repos.ConceptMappingOverrideRepo,
	evidence repos.ConceptEvidenceRepo,
	edges repos.ConceptEdgeRepo,
	graph *neo4jdb.Client,
	ai openai.Client,
	vec pinecone.VectorStore,
	saga services.SagaService,
	bootstrap services.LearningBuildBootstrapService,
	artifacts repos.LearningArtifactRepo,
) *Pipeline {
	return &Pipeline{
		db:        db,
		log:       baseLog.With("job", "concept_graph_build"),
		files:     files,
		fileSigs:  fileSigs,
		chunks:    chunks,
		path:      path,
		concepts:  concepts,
		reps:      reps,
		overrides: overrides,
		evidence:  evidence,
		edges:     edges,
		graph:     graph,
		ai:        ai,
		vec:       vec,
		saga:      saga,
		bootstrap: bootstrap,
		artifacts: artifacts,
	}
}

func (p *Pipeline) Type() string { return "concept_graph_build" }
