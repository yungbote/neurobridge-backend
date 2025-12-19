package chain_signature_build

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
	concepts  repos.ConceptRepo
	clusters  repos.ConceptClusterRepo
	members   repos.ConceptClusterMemberRepo
	edges     repos.ConceptEdgeRepo
	chains    repos.ChainSignatureRepo
	ai        openai.Client
	vec       pinecone.VectorStore
	saga      services.SagaService
	bootstrap services.LearningBuildBootstrapService
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
	}
}

func (p *Pipeline) Type() string { return "chain_signature_build" }
