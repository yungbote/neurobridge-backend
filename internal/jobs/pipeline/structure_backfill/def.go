package structure_backfill

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type Pipeline struct {
	db        *gorm.DB
	log       *logger.Logger
	path      repos.PathRepo
	nodes     repos.PathNodeRepo
	concepts  repos.ConceptRepo
	psus      repos.PathStructuralUnitRepo
	bootstrap services.LearningBuildBootstrapService
	mastery   repos.UserConceptStateRepo
	model     repos.UserConceptModelRepo
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	path repos.PathRepo,
	nodes repos.PathNodeRepo,
	concepts repos.ConceptRepo,
	psus repos.PathStructuralUnitRepo,
	bootstrap services.LearningBuildBootstrapService,
	mastery repos.UserConceptStateRepo,
	model repos.UserConceptModelRepo,
) *Pipeline {
	return &Pipeline{
		db:        db,
		log:       baseLog.With("job", "structure_backfill"),
		path:      path,
		nodes:     nodes,
		concepts:  concepts,
		psus:      psus,
		bootstrap: bootstrap,
		mastery:   mastery,
		model:     model,
	}
}

func (p *Pipeline) Type() string { return "structure_backfill" }
