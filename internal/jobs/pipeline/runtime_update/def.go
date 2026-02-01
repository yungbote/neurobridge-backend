package runtime_update

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type Pipeline struct {
	db        *gorm.DB
	log       *logger.Logger
	events    repos.UserEventRepo
	cursors   repos.UserEventCursorRepo
	paths     repos.PathRepo
	pathNodes repos.PathNodeRepo
	nodeActs  repos.PathNodeActivityRepo
	pathRuns  repos.PathRunRepo
	nodeRuns  repos.NodeRunRepo
	actRuns   repos.ActivityRunRepo
	trans     repos.PathRunTransitionRepo
	sessions  repos.UserSessionStateRepo
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	events repos.UserEventRepo,
	cursors repos.UserEventCursorRepo,
	paths repos.PathRepo,
	pathNodes repos.PathNodeRepo,
	nodeActs repos.PathNodeActivityRepo,
	pathRuns repos.PathRunRepo,
	nodeRuns repos.NodeRunRepo,
	actRuns repos.ActivityRunRepo,
	trans repos.PathRunTransitionRepo,
	sessions repos.UserSessionStateRepo,
) *Pipeline {
	return &Pipeline{
		db:        db,
		log:       baseLog.With("job", "runtime_update"),
		events:    events,
		cursors:   cursors,
		paths:     paths,
		pathNodes: pathNodes,
		nodeActs:  nodeActs,
		pathRuns:  pathRuns,
		nodeRuns:  nodeRuns,
		actRuns:   actRuns,
		trans:     trans,
		sessions:  sessions,
	}
}

func (p *Pipeline) Type() string { return "runtime_update" }
