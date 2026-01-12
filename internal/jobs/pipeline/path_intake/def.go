package path_intake

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type Pipeline struct {
	db        *gorm.DB
	log       *logger.Logger
	files     repos.MaterialFileRepo
	chunks    repos.MaterialChunkRepo
	summaries repos.MaterialSetSummaryRepo
	path      repos.PathRepo
	prefs     repos.UserPersonalizationPrefsRepo
	threads   repos.ChatThreadRepo
	messages  repos.ChatMessageRepo
	ai        openai.Client
	notify    services.ChatNotifier
	bootstrap services.LearningBuildBootstrapService
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	files repos.MaterialFileRepo,
	chunks repos.MaterialChunkRepo,
	summaries repos.MaterialSetSummaryRepo,
	path repos.PathRepo,
	prefs repos.UserPersonalizationPrefsRepo,
	threads repos.ChatThreadRepo,
	messages repos.ChatMessageRepo,
	ai openai.Client,
	notify services.ChatNotifier,
	bootstrap services.LearningBuildBootstrapService,
) *Pipeline {
	return &Pipeline{
		db:        db,
		log:       baseLog.With("job", "path_intake"),
		files:     files,
		chunks:    chunks,
		summaries: summaries,
		path:      path,
		prefs:     prefs,
		threads:   threads,
		messages:  messages,
		ai:        ai,
		notify:    notify,
		bootstrap: bootstrap,
	}
}

func (p *Pipeline) Type() string { return "path_intake" }
