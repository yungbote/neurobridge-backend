package waitpoint_stage

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type Pipeline struct {
	db       *gorm.DB
	log      *logger.Logger
	threads  repos.ChatThreadRepo
	messages repos.ChatMessageRepo
	notify   services.ChatNotifier
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	threads repos.ChatThreadRepo,
	messages repos.ChatMessageRepo,
	notify services.ChatNotifier,
) *Pipeline {
	return &Pipeline{
		db:       db,
		log:      baseLog.With("job", "waitpoint_stage"),
		threads:  threads,
		messages: messages,
		notify:   notify,
	}
}

func (p *Pipeline) Type() string { return "waitpoint_stage" }
