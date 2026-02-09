package doc_variant_eval

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type Pipeline struct {
	db           *gorm.DB
	log          *logger.Logger
	exposures    repos.DocVariantExposureRepo
	outcomes     repos.DocVariantOutcomeRepo
	nodeRuns     repos.NodeRunRepo
	conceptState repos.UserConceptStateRepo
	bootstrap    services.LearningBuildBootstrapService
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	exposures repos.DocVariantExposureRepo,
	outcomes repos.DocVariantOutcomeRepo,
	nodeRuns repos.NodeRunRepo,
	conceptState repos.UserConceptStateRepo,
	bootstrap services.LearningBuildBootstrapService,
) *Pipeline {
	return &Pipeline{
		db:           db,
		log:          baseLog.With("job", "doc_variant_eval"),
		exposures:    exposures,
		outcomes:     outcomes,
		nodeRuns:     nodeRuns,
		conceptState: conceptState,
		bootstrap:    bootstrap,
	}
}

func (p *Pipeline) Type() string { return "doc_variant_eval" }
