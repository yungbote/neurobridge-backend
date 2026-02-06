package runtime_update

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/observability"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type Pipeline struct {
	db        *gorm.DB
	log       *logger.Logger
	events    repos.UserEventRepo
	cursors   repos.UserEventCursorRepo
	paths     repos.PathRepo
	pathNodes repos.PathNodeRepo
	nodeActs  repos.PathNodeActivityRepo
	nodeDocs  repos.LearningNodeDocRepo
	pathRuns  repos.PathRunRepo
	nodeRuns  repos.NodeRunRepo
	actRuns   repos.ActivityRunRepo
	trans     repos.PathRunTransitionRepo
	sessions  repos.UserSessionStateRepo
	concepts  repos.ConceptRepo
	conStates repos.UserConceptStateRepo
	miscons   repos.UserMisconceptionInstanceRepo
	testlets  repos.UserTestletStateRepo
	traces    repos.DecisionTraceRepo
	models    repos.ModelSnapshotRepo
	evals     repos.PolicyEvalSnapshotRepo
	jobSvc    services.JobService
	notify    services.RuntimeNotifier
	metrics   *observability.Metrics
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	events repos.UserEventRepo,
	cursors repos.UserEventCursorRepo,
	paths repos.PathRepo,
	pathNodes repos.PathNodeRepo,
	nodeActs repos.PathNodeActivityRepo,
	nodeDocs repos.LearningNodeDocRepo,
	pathRuns repos.PathRunRepo,
	nodeRuns repos.NodeRunRepo,
	actRuns repos.ActivityRunRepo,
	trans repos.PathRunTransitionRepo,
	sessions repos.UserSessionStateRepo,
	concepts repos.ConceptRepo,
	conStates repos.UserConceptStateRepo,
	miscons repos.UserMisconceptionInstanceRepo,
	testlets repos.UserTestletStateRepo,
	traces repos.DecisionTraceRepo,
	models repos.ModelSnapshotRepo,
	evals repos.PolicyEvalSnapshotRepo,
	jobSvc services.JobService,
	notify services.RuntimeNotifier,
	metrics *observability.Metrics,
) *Pipeline {
	return &Pipeline{
		db:        db,
		log:       baseLog.With("job", "runtime_update"),
		events:    events,
		cursors:   cursors,
		paths:     paths,
		pathNodes: pathNodes,
		nodeActs:  nodeActs,
		nodeDocs:  nodeDocs,
		pathRuns:  pathRuns,
		nodeRuns:  nodeRuns,
		actRuns:   actRuns,
		trans:     trans,
		sessions:  sessions,
		concepts:  concepts,
		conStates: conStates,
		miscons:   miscons,
		testlets:  testlets,
		traces:    traces,
		models:    models,
		evals:     evals,
		jobSvc:    jobSvc,
		notify:    notify,
		metrics:   metrics,
	}
}

func (p *Pipeline) Type() string { return "runtime_update" }
