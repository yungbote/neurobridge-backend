package chat_waitpoint_interpret

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

// Pipeline interprets a thread-scoped waitpoint (e.g., node_doc_edit) and
// dispatches follow-on jobs (apply/deny/refine or chat_respond).
type Pipeline struct {
	db  *gorm.DB
	log *logger.Logger

	ai openai.Client

	threads  repos.ChatThreadRepo
	messages repos.ChatMessageRepo
	turns    repos.ChatTurnRepo
	jobRuns  repos.JobRunRepo
	jobs     services.JobService

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
	notify services.ChatNotifier,
) *Pipeline {
	return &Pipeline{
		db:       db,
		log:      baseLog.With("job", "chat_waitpoint_interpret"),
		ai:       ai,
		threads:  threads,
		messages: messages,
		turns:    turns,
		jobRuns:  jobRuns,
		jobs:     jobs,
		notify:   notify,
	}
}

func (p *Pipeline) Type() string {
	return "chat_waitpoint_interpret"
}
