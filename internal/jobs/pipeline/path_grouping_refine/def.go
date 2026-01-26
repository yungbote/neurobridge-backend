package path_grouping_refine

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type Pipeline struct {
	db      *gorm.DB
	log     *logger.Logger
	path    repos.PathRepo
	files   repos.MaterialFileRepo
	fileSigs repos.MaterialFileSignatureRepo
	prefs   repos.UserPersonalizationPrefsRepo
	threads repos.ChatThreadRepo
	messages repos.ChatMessageRepo
	notify services.ChatNotifier
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	path repos.PathRepo,
	files repos.MaterialFileRepo,
	fileSigs repos.MaterialFileSignatureRepo,
	prefs repos.UserPersonalizationPrefsRepo,
	threads repos.ChatThreadRepo,
	messages repos.ChatMessageRepo,
	notify services.ChatNotifier,
) *Pipeline {
	return &Pipeline{
		db:      db,
		log:     baseLog.With("job", "path_grouping_refine"),
		path:    path,
		files:   files,
		fileSigs: fileSigs,
		prefs:   prefs,
		threads: threads,
		messages: messages,
		notify:  notify,
	}
}

func (p *Pipeline) Type() string { return "path_grouping_refine" }
