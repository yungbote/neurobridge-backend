package user_model_update

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type Pipeline struct {
	db  *gorm.DB
	log *logger.Logger

	events       repos.UserEventRepo
	cursors      repos.UserEventCursorRepo
	conceptState repos.UserConceptStateRepo
	stylePrefs   repos.UserStylePreferenceRepo

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
	jobRuns repos.JobRunRepo,
) *Pipeline {
	return &Pipeline{
		db:           db,
		log:          baseLog.With("job", "user_model_update"),
		events:       events,
		cursors:      cursors,
		conceptState: conceptState,
		stylePrefs:   stylePrefs,
		jobRuns:      jobRuns,
	}
}

func (p *Pipeline) Type() string { return "user_model_update" }
