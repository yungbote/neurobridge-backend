package file_signature_build

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
	"github.com/yungbote/neurobridge-backend/internal/platform/pinecone"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type Pipeline struct {
	db           *gorm.DB
	log          *logger.Logger
	files        repos.MaterialFileRepo
	fileSigs     repos.MaterialFileSignatureRepo
	fileSections repos.MaterialFileSectionRepo
	chunks       repos.MaterialChunkRepo
	ai           openai.Client
	vec          pinecone.VectorStore
	saga         services.SagaService
	bootstrap    services.LearningBuildBootstrapService
	artifacts    repos.LearningArtifactRepo
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	files repos.MaterialFileRepo,
	fileSigs repos.MaterialFileSignatureRepo,
	fileSections repos.MaterialFileSectionRepo,
	chunks repos.MaterialChunkRepo,
	ai openai.Client,
	vec pinecone.VectorStore,
	saga services.SagaService,
	bootstrap services.LearningBuildBootstrapService,
	artifacts repos.LearningArtifactRepo,
) *Pipeline {
	return &Pipeline{
		db:           db,
		log:          baseLog.With("job", "file_signature_build"),
		files:        files,
		fileSigs:     fileSigs,
		fileSections: fileSections,
		chunks:       chunks,
		ai:           ai,
		vec:          vec,
		saga:         saga,
		bootstrap:    bootstrap,
		artifacts:    artifacts,
	}
}

func (p *Pipeline) Type() string { return "file_signature_build" }
