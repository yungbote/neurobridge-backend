package chat_rebuild

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/clients/pinecone"
	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type Pipeline struct {
	db  *gorm.DB
	log *logger.Logger

	vec pinecone.VectorStore

	jobRuns repos.JobRunRepo
	jobs    services.JobService
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	vec pinecone.VectorStore,
	jobRuns repos.JobRunRepo,
	jobs services.JobService,
) *Pipeline {
	return &Pipeline{
		db:      db,
		log:     baseLog.With("job", "chat_rebuild"),
		vec:     vec,
		jobRuns: jobRuns,
		jobs:    jobs,
	}
}

func (p *Pipeline) Type() string { return "chat_rebuild" }

