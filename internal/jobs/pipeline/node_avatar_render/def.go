package node_avatar_render

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type Pipeline struct {
	db        *gorm.DB
	log       *logger.Logger
	path      repos.PathRepo
	nodes     repos.PathNodeRepo
	avatar    services.AvatarService
	bootstrap services.LearningBuildBootstrapService
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	path repos.PathRepo,
	nodes repos.PathNodeRepo,
	avatar services.AvatarService,
	bootstrap services.LearningBuildBootstrapService,
) *Pipeline {
	return &Pipeline{
		db:        db,
		log:       baseLog.With("job", "node_avatar_render"),
		path:      path,
		nodes:     nodes,
		avatar:    avatar,
		bootstrap: bootstrap,
	}
}

func (p *Pipeline) Type() string { return "node_avatar_render" }
