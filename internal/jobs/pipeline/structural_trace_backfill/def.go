package structural_trace_backfill

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type Pipeline struct {
	db  *gorm.DB
	log *logger.Logger
}

func New(db *gorm.DB, baseLog *logger.Logger) *Pipeline {
	return &Pipeline{
		db:  db,
		log: baseLog.With("job", "structural_trace_backfill"),
	}
}

func (p *Pipeline) Type() string { return "structural_trace_backfill" }
