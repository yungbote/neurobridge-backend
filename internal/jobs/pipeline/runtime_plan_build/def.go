package runtime_plan_build

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type Pipeline struct {
	db         *gorm.DB
	log        *logger.Logger
	path       repos.PathRepo
	nodes      repos.PathNodeRepo
	nodeDocs   repos.LearningNodeDocRepo
	summaries  repos.MaterialSetSummaryRepo
	userProf   repos.UserProfileVectorRepo
	progEvents repos.UserProgressionEventRepo
	ai         openai.Client
	bootstrap  services.LearningBuildBootstrapService
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	path repos.PathRepo,
	nodes repos.PathNodeRepo,
	nodeDocs repos.LearningNodeDocRepo,
	summaries repos.MaterialSetSummaryRepo,
	userProf repos.UserProfileVectorRepo,
	progEvents repos.UserProgressionEventRepo,
	ai openai.Client,
	bootstrap services.LearningBuildBootstrapService,
) *Pipeline {
	return &Pipeline{
		db:         db,
		log:        baseLog.With("job", "runtime_plan_build"),
		path:       path,
		nodes:      nodes,
		nodeDocs:   nodeDocs,
		summaries:  summaries,
		userProf:   userProf,
		progEvents: progEvents,
		ai:         ai,
		bootstrap:  bootstrap,
	}
}

func (p *Pipeline) Type() string { return "runtime_plan_build" }
