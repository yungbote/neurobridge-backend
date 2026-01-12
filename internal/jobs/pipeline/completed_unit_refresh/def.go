package completed_unit_refresh

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/neo4jdb"
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
	graph     *neo4jdb.Client
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
	graph *neo4jdb.Client,
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
		graph:     graph,
		bootstrap: bootstrap,
	}
}

func (p *Pipeline) Type() string { return "completed_unit_refresh" }
