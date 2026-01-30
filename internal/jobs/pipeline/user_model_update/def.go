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

	events       repos.UserEventRepo
	cursors      repos.UserEventCursorRepo
	concepts     repos.ConceptRepo
	actConcepts  repos.ActivityConceptRepo
	conceptState repos.UserConceptStateRepo
	conceptModel repos.UserConceptModelRepo
	misconRepo   repos.UserMisconceptionInstanceRepo
	stylePrefs   repos.UserStylePreferenceRepo
	graph        *neo4jdb.Client

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
	actConcepts repos.ActivityConceptRepo,
	conceptState repos.UserConceptStateRepo,
	conceptModel repos.UserConceptModelRepo,
	misconRepo repos.UserMisconceptionInstanceRepo,
	stylePrefs repos.UserStylePreferenceRepo,
	graph *neo4jdb.Client,
	jobRuns repos.JobRunRepo,
	jobSvc services.JobService,
) *Pipeline {
	return &Pipeline{
		db:           db,
		log:          baseLog.With("job", "user_model_update"),
		events:       events,
		cursors:      cursors,
		concepts:     concepts,
		actConcepts:  actConcepts,
		conceptState: conceptState,
		conceptModel: conceptModel,
		misconRepo:   misconRepo,
		stylePrefs:   stylePrefs,
		graph:        graph,
		jobRuns:      jobRuns,
		jobSvc:       jobSvc,
	}
}

func (p *Pipeline) Type() string { return "user_model_update" }
