package graph_version_rollback

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type Pipeline struct {
	db        *gorm.DB
	log       *logger.Logger
	graphs    repos.GraphVersionRepo
	rollbacks repos.RollbackEventRepo
	jobRuns   repos.JobRunRepo
	jobSvc    services.JobService
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	graphs repos.GraphVersionRepo,
	rollbacks repos.RollbackEventRepo,
	jobRuns repos.JobRunRepo,
	jobSvc services.JobService,
) *Pipeline {
	return &Pipeline{
		db:        db,
		log:       baseLog.With("job", "graph_version_rollback"),
		graphs:    graphs,
		rollbacks: rollbacks,
		jobRuns:   jobRuns,
		jobSvc:    jobSvc,
	}
}

func (p *Pipeline) Type() string { return "graph_version_rollback" }
