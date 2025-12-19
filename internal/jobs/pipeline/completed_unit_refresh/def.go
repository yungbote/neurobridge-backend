package completed_unit_refresh

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type Pipeline struct {
	db        *gorm.DB
	log       *logger.Logger
	completed repos.UserCompletedUnitRepo
	progress  repos.UserProgressionEventRepo
	concepts  repos.ConceptRepo
	act       repos.ActivityRepo
	actCon    repos.ActivityConceptRepo
	chains    repos.ChainSignatureRepo
	mastery   repos.UserConceptStateRepo
	bootstrap services.LearningBuildBootstrapService
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	completed repos.UserCompletedUnitRepo,
	progress repos.UserProgressionEventRepo,
	concepts repos.ConceptRepo,
	act repos.ActivityRepo,
	actCon repos.ActivityConceptRepo,
	chains repos.ChainSignatureRepo,
	mastery repos.UserConceptStateRepo,
	bootstrap services.LearningBuildBootstrapService,
) *Pipeline {
	return &Pipeline{
		db:        db,
		log:       baseLog.With("job", "completed_unit_refresh"),
		completed: completed,
		progress:  progress,
		concepts:  concepts,
		act:       act,
		actCon:    actCon,
		chains:    chains,
		mastery:   mastery,
		bootstrap: bootstrap,
	}
}

func (p *Pipeline) Type() string { return "completed_unit_refresh" }
