package user_model_update

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/neo4jdb"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type Pipeline struct {
	db  *gorm.DB
	log *logger.Logger

	events         repos.UserEventRepo
	cursors        repos.UserEventCursorRepo
	concepts       repos.ConceptRepo
	edges          repos.ConceptEdgeRepo
	clusterMembers repos.ConceptClusterMemberRepo
	actConcepts    repos.ActivityConceptRepo
	conceptState   repos.UserConceptStateRepo
	conceptModel   repos.UserConceptModelRepo
	edgeStats      repos.UserConceptEdgeStatRepo
	evidenceRepo   repos.UserConceptEvidenceRepo
	calibRepo      repos.UserConceptCalibrationRepo
	itemCalib      repos.ItemCalibrationRepo
	alertRepo      repos.UserModelAlertRepo
	misconRepo     repos.UserMisconceptionInstanceRepo
	misconEdges    repos.MisconceptionCausalEdgeRepo
	stylePrefs     repos.UserStylePreferenceRepo
	testletState   repos.UserTestletStateRepo
	skillState     repos.UserSkillStateRepo
	graph          *neo4jdb.Client

	// kept for future expansion / wiring compatibility
	jobRuns repos.JobRunRepo
	jobSvc  services.JobService
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	events repos.UserEventRepo,
	cursors repos.UserEventCursorRepo,
	concepts repos.ConceptRepo,
	edges repos.ConceptEdgeRepo,
	actConcepts repos.ActivityConceptRepo,
	conceptState repos.UserConceptStateRepo,
	conceptModel repos.UserConceptModelRepo,
	edgeStats repos.UserConceptEdgeStatRepo,
	evidenceRepo repos.UserConceptEvidenceRepo,
	calibRepo repos.UserConceptCalibrationRepo,
	itemCalib repos.ItemCalibrationRepo,
	alertRepo repos.UserModelAlertRepo,
	misconRepo repos.UserMisconceptionInstanceRepo,
	misconEdges repos.MisconceptionCausalEdgeRepo,
	stylePrefs repos.UserStylePreferenceRepo,
	clusterMembers repos.ConceptClusterMemberRepo,
	testletState repos.UserTestletStateRepo,
	skillState repos.UserSkillStateRepo,
	graph *neo4jdb.Client,
	jobRuns repos.JobRunRepo,
	jobSvc services.JobService,
) *Pipeline {
	return &Pipeline{
		db:             db,
		log:            baseLog.With("job", "user_model_update"),
		events:         events,
		cursors:        cursors,
		concepts:       concepts,
		edges:          edges,
		clusterMembers: clusterMembers,
		actConcepts:    actConcepts,
		conceptState:   conceptState,
		conceptModel:   conceptModel,
		edgeStats:      edgeStats,
		evidenceRepo:   evidenceRepo,
		calibRepo:      calibRepo,
		itemCalib:      itemCalib,
		alertRepo:      alertRepo,
		misconRepo:     misconRepo,
		misconEdges:    misconEdges,
		stylePrefs:     stylePrefs,
		testletState:   testletState,
		skillState:     skillState,
		graph:          graph,
		jobRuns:        jobRuns,
		jobSvc:         jobSvc,
	}
}

func (p *Pipeline) Type() string { return "user_model_update" }
