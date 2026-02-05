package structure_extract

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type Pipeline struct {
	db       *gorm.DB
	log      *logger.Logger
	ai       openai.Client
	threads  repos.ChatThreadRepo
	messages repos.ChatMessageRepo
	state    repos.ChatThreadStateRepo
	concepts repos.ConceptRepo
	model    repos.UserConceptModelRepo
	miscon   repos.UserMisconceptionInstanceRepo
	events   repos.UserEventRepo
	jobs     services.JobService
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	ai openai.Client,
	threads repos.ChatThreadRepo,
	messages repos.ChatMessageRepo,
	state repos.ChatThreadStateRepo,
	concepts repos.ConceptRepo,
	model repos.UserConceptModelRepo,
	miscon repos.UserMisconceptionInstanceRepo,
	events repos.UserEventRepo,
	jobs services.JobService,
) *Pipeline {
	return &Pipeline{
		db:       db,
		log:      baseLog.With("job", "structure_extract"),
		ai:       ai,
		threads:  threads,
		messages: messages,
		state:    state,
		concepts: concepts,
		model:    model,
		miscon:   miscon,
		events:   events,
		jobs:     jobs,
	}
}

func (p *Pipeline) Type() string { return "structure_extract" }
