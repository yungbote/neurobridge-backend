package psu_build

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type Pipeline struct {
	db        *gorm.DB
	log       *logger.Logger
	nodes     repos.PathNodeRepo
	concepts  repos.ConceptRepo
	psus      repos.PathStructuralUnitRepo
	bootstrap services.LearningBuildBootstrapService
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	nodes repos.PathNodeRepo,
	concepts repos.ConceptRepo,
	psus repos.PathStructuralUnitRepo,
	bootstrap services.LearningBuildBootstrapService,
) *Pipeline {
	return &Pipeline{
		db:        db,
		log:       baseLog.With("job", "psu_build"),
		nodes:     nodes,
		concepts:  concepts,
		psus:      psus,
		bootstrap: bootstrap,
	}
}

func (p *Pipeline) Type() string { return "psu_build" }
