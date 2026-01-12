package web_resources_seed

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/platform/gcp"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type Pipeline struct {
	db        *gorm.DB
	log       *logger.Logger
	files     repos.MaterialFileRepo
	path      repos.PathRepo
	bucket    gcp.BucketService
	threads   repos.ChatThreadRepo
	messages  repos.ChatMessageRepo
	notify    services.ChatNotifier
	ai        openai.Client
	saga      services.SagaService
	bootstrap services.LearningBuildBootstrapService
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	files repos.MaterialFileRepo,
	path repos.PathRepo,
	bucket gcp.BucketService,
	threads repos.ChatThreadRepo,
	messages repos.ChatMessageRepo,
	notify services.ChatNotifier,
	ai openai.Client,
	saga services.SagaService,
	bootstrap services.LearningBuildBootstrapService,
) *Pipeline {
	return &Pipeline{
		db:        db,
		log:       baseLog.With("job", "web_resources_seed"),
		files:     files,
		path:      path,
		bucket:    bucket,
		threads:   threads,
		messages:  messages,
		notify:    notify,
		ai:        ai,
		saga:      saga,
		bootstrap: bootstrap,
	}
}

func (p *Pipeline) Type() string { return "web_resources_seed" }
