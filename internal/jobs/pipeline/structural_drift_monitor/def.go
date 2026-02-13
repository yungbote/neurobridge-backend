package structural_drift_monitor

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type Pipeline struct {
	db           *gorm.DB
	log          *logger.Logger
	metrics      repos.StructuralDriftMetricRepo
	rollbackRepo repos.RollbackEventRepo
}

func New(db *gorm.DB, baseLog *logger.Logger, metrics repos.StructuralDriftMetricRepo, rollbackRepo repos.RollbackEventRepo) *Pipeline {
	return &Pipeline{
		db:           db,
		log:          baseLog.With("job", "structural_drift_monitor"),
		metrics:      metrics,
		rollbackRepo: rollbackRepo,
	}
}

func (p *Pipeline) Type() string { return "structural_drift_monitor" }
