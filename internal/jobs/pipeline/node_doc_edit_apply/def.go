package node_doc_edit_apply

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type Pipeline struct {
	db        *gorm.DB
	log       *logger.Logger
	jobs      repos.JobRunRepo
	jobSvc    services.JobService
	threads   repos.ChatThreadRepo
	nodes     repos.PathNodeRepo
	docs      repos.LearningNodeDocRepo
	revisions repos.LearningNodeDocRevisionRepo
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	jobs repos.JobRunRepo,
	jobSvc services.JobService,
	threads repos.ChatThreadRepo,
	nodes repos.PathNodeRepo,
	docs repos.LearningNodeDocRepo,
	revisions repos.LearningNodeDocRevisionRepo,
) *Pipeline {
	return &Pipeline{
		db:        db,
		log:       baseLog.With("job", "node_doc_edit_apply"),
		jobs:      jobs,
		jobSvc:    jobSvc,
		threads:   threads,
		nodes:     nodes,
		docs:      docs,
		revisions: revisions,
	}
}

func (p *Pipeline) Type() string { return "node_doc_edit_apply" }
