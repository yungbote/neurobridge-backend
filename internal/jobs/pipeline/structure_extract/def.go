package structure_extract

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
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
	}
}

func (p *Pipeline) Type() string { return "structure_extract" }
