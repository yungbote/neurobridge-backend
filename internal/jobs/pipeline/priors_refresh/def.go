package priors_refresh

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type Pipeline struct {
	db         *gorm.DB
	log        *logger.Logger
	activities repos.ActivityRepo
	variants   repos.ActivityVariantRepo
	stats      repos.ActivityVariantStatRepo
	chains     repos.ChainSignatureRepo
	concepts   repos.ConceptRepo
	actConcept repos.ActivityConceptRepo
	chain      repos.ChainPriorRepo
	cohort     repos.CohortPriorRepo
	bootstrap  services.LearningBuildBootstrapService
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	activities repos.ActivityRepo,
	variants repos.ActivityVariantRepo,
	stats repos.ActivityVariantStatRepo,
	chains repos.ChainSignatureRepo,
	concepts repos.ConceptRepo,
	actConcept repos.ActivityConceptRepo,
	chain repos.ChainPriorRepo,
	cohort repos.CohortPriorRepo,
	bootstrap services.LearningBuildBootstrapService,
) *Pipeline {
	return &Pipeline{
		db:         db,
		log:        baseLog.With("job", "priors_refresh"),
		activities: activities,
		variants:   variants,
		stats:      stats,
		chains:     chains,
		concepts:   concepts,
		actConcept: actConcept,
		chain:      chain,
		cohort:     cohort,
		bootstrap:  bootstrap,
	}
}

func (p *Pipeline) Type() string { return "priors_refresh" }
