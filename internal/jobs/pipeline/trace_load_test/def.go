package trace_load_test

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/observability"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type Pipeline struct {
	db      *gorm.DB
	log     *logger.Logger
	metrics *observability.Metrics
}

func New(db *gorm.DB, baseLog *logger.Logger, metrics *observability.Metrics) *Pipeline {
	return &Pipeline{
		db:      db,
		log:     baseLog.With("job", "trace_load_test"),
		metrics: metrics,
	}
}

func (p *Pipeline) Type() string { return "trace_load_test" }
