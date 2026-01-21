package waitpoint_interpret

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

// Pipeline executes interpretation of a paused waitpoint using
// step-specific configuration and LLM-based classification.
type Pipeline struct {
	db  *gorm.DB
	log *logger.Logger

	ai openai.Client

	threads  repos.ChatThreadRepo
	messages repos.ChatMessageRepo
	turns    repos.ChatTurnRepo

	jobRuns repos.JobRunRepo
	jobs    services.JobService

	path repos.PathRepo

	notify services.ChatNotifier
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	ai openai.Client,
	threads repos.ChatThreadRepo,
	messages repos.ChatMessageRepo,
	turns repos.ChatTurnRepo,
	jobRuns repos.JobRunRepo,
	jobs services.JobService,
	path repos.PathRepo,
	notify services.ChatNotifier,
) *Pipeline {
	return &Pipeline{
		db:       db,
		log:      baseLog.With("job", "waitpoint_interpret"),
		ai:       ai,
		threads:  threads,
		messages: messages,
		turns:    turns,
		jobRuns:  jobRuns,
		jobs:     jobs,
		path:     path,
		notify:   notify,
	}
}

func (p *Pipeline) Type() string {
	return "waitpoint_interpret"
}









