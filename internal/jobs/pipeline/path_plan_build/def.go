package path_plan_build

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/neo4jdb"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type Pipeline struct {
	db        *gorm.DB
	log       *logger.Logger
	path      repos.PathRepo
	nodes     repos.PathNodeRepo
	concepts  repos.ConceptRepo
	edges     repos.ConceptEdgeRepo
	summaries repos.MaterialSetSummaryRepo
	profile   repos.UserProfileVectorRepo
	mastery   repos.UserConceptStateRepo
	graph     *neo4jdb.Client
	ai        openai.Client
	bootstrap services.LearningBuildBootstrapService
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	path repos.PathRepo,
	nodes repos.PathNodeRepo,
	concepts repos.ConceptRepo,
	edges repos.ConceptEdgeRepo,
	summaries repos.MaterialSetSummaryRepo,
	profile repos.UserProfileVectorRepo,
	mastery repos.UserConceptStateRepo,
	graph *neo4jdb.Client,
	ai openai.Client,
	bootstrap services.LearningBuildBootstrapService,
) *Pipeline {
	return &Pipeline{
		db:        db,
		log:       baseLog.With("job", "path_plan_build"),
		path:      path,
		nodes:     nodes,
		concepts:  concepts,
		edges:     edges,
		summaries: summaries,
		profile:   profile,
		mastery:   mastery,
		graph:     graph,
		ai:        ai,
		bootstrap: bootstrap,
	}
}

func (p *Pipeline) Type() string { return "path_plan_build" }
