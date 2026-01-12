package progression_compact

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type Pipeline struct {
	db        *gorm.DB
	log       *logger.Logger
	events    repos.UserEventRepo
	cursors   repos.UserEventCursorRepo
	progress  repos.UserProgressionEventRepo
	bootstrap services.LearningBuildBootstrapService
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	events repos.UserEventRepo,
	cursors repos.UserEventCursorRepo,
	progress repos.UserProgressionEventRepo,
	bootstrap services.LearningBuildBootstrapService,
) *Pipeline {
	return &Pipeline{
		db:        db,
		log:       baseLog.With("job", "progression_compact"),
		events:    events,
		cursors:   cursors,
		progress:  progress,
		bootstrap: bootstrap,
	}
}

func (p *Pipeline) Type() string { return "progression_compact" }
