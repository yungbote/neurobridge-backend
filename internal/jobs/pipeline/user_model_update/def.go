package user_model_update

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/neo4jdb"
)

type Pipeline struct {
	db  *gorm.DB
	log *logger.Logger

	events       repos.UserEventRepo
	cursors      repos.UserEventCursorRepo
	conceptState repos.UserConceptStateRepo
	stylePrefs   repos.UserStylePreferenceRepo
	graph        *neo4jdb.Client

	// kept for future expansion / wiring compatibility
	jobRuns repos.JobRunRepo
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	events repos.UserEventRepo,
	cursors repos.UserEventCursorRepo,
	conceptState repos.UserConceptStateRepo,
	stylePrefs repos.UserStylePreferenceRepo,
	graph *neo4jdb.Client,
	jobRuns repos.JobRunRepo,
) *Pipeline {
	return &Pipeline{
		db:           db,
		log:          baseLog.With("job", "user_model_update"),
		events:       events,
		cursors:      cursors,
		conceptState: conceptState,
		stylePrefs:   stylePrefs,
		graph:        graph,
		jobRuns:      jobRuns,
	}
}

func (p *Pipeline) Type() string { return "user_model_update" }
