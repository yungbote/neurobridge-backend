package chain_signature_build

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
	edges     repos.ConceptEdgeRepo
	chains    repos.ChainSignatureRepo
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
	edges repos.ConceptEdgeRepo,
	chains repos.ChainSignatureRepo,
	ai openai.Client,
	vec pinecone.VectorStore,
	saga services.SagaService,
	bootstrap services.LearningBuildBootstrapService,
	artifacts repos.LearningArtifactRepo,
) *Pipeline {
	return &Pipeline{
		db:        db,
		log:       baseLog.With("job", "chain_signature_build"),
		concepts:  concepts,
		clusters:  clusters,
		members:   members,
		edges:     edges,
		chains:    chains,
		ai:        ai,
		vec:       vec,
		saga:      saga,
		bootstrap: bootstrap,
		artifacts: artifacts,
	}
}

func (p *Pipeline) Type() string { return "chain_signature_build" }
