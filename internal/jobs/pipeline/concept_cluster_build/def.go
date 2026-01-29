package concept_cluster_build

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
	clusters  repos.ConceptClusterRepo
	members   repos.ConceptClusterMemberRepo
	ai        openai.Client
	vec       pinecone.VectorStore
	saga      services.SagaService
	bootstrap services.LearningBuildBootstrapService
	artifacts repos.LearningArtifactRepo
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	concepts repos.ConceptRepo,
	clusters repos.ConceptClusterRepo,
	members repos.ConceptClusterMemberRepo,
	ai openai.Client,
	vec pinecone.VectorStore,
	saga services.SagaService,
	bootstrap services.LearningBuildBootstrapService,
	artifacts repos.LearningArtifactRepo,
) *Pipeline {
	return &Pipeline{
		db:        db,
		log:       baseLog.With("job", "concept_cluster_build"),
		concepts:  concepts,
		clusters:  clusters,
		members:   members,
		ai:        ai,
		vec:       vec,
		saga:      saga,
		bootstrap: bootstrap,
		artifacts: artifacts,
	}
}

func (p *Pipeline) Type() string { return "concept_cluster_build" }
