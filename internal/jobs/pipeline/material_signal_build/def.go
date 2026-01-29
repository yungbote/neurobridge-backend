package material_signal_build

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type Pipeline struct {
	db           *gorm.DB
	log          *logger.Logger
	files        repos.MaterialFileRepo
	fileSigs     repos.MaterialFileSignatureRepo
	fileSections repos.MaterialFileSectionRepo
	chunks       repos.MaterialChunkRepo
	concepts     repos.ConceptRepo
	materialSets repos.MaterialSetRepo
	ai           openai.Client
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
	concepts repos.ConceptRepo,
	materialSets repos.MaterialSetRepo,
	ai openai.Client,
	bootstrap services.LearningBuildBootstrapService,
	artifacts repos.LearningArtifactRepo,
) *Pipeline {
	return &Pipeline{
		db:           db,
		log:          baseLog.With("job", "material_signal_build"),
		files:        files,
		fileSigs:     fileSigs,
		fileSections: fileSections,
		chunks:       chunks,
		concepts:     concepts,
		materialSets: materialSets,
		ai:           ai,
		bootstrap:    bootstrap,
		artifacts:    artifacts,
	}
}

func (p *Pipeline) Type() string { return "material_signal_build" }
