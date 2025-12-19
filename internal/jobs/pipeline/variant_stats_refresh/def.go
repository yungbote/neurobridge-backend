package variant_stats_refresh

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type Pipeline struct {
	db        *gorm.DB
	log       *logger.Logger
	events    repos.UserEventRepo
	cursors   repos.UserEventCursorRepo
	variants  repos.ActivityVariantRepo
	stats     repos.ActivityVariantStatRepo
	bootstrap services.LearningBuildBootstrapService
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	events repos.UserEventRepo,
	cursors repos.UserEventCursorRepo,
	variants repos.ActivityVariantRepo,
	stats repos.ActivityVariantStatRepo,
	bootstrap services.LearningBuildBootstrapService,
) *Pipeline {
	return &Pipeline{
		db:        db,
		log:       baseLog.With("job", "variant_stats_refresh"),
		events:    events,
		cursors:   cursors,
		variants:  variants,
		stats:     stats,
		bootstrap: bootstrap,
	}
}

func (p *Pipeline) Type() string { return "variant_stats_refresh" }
