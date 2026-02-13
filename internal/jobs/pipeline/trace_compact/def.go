package trace_compact

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
		log: baseLog.With("job", "trace_compact"),
	}
}

func (p *Pipeline) Type() string { return "trace_compact" }
