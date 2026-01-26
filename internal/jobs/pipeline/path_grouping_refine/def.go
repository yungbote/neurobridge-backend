package path_grouping_refine

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type Pipeline struct {
	db      *gorm.DB
	log     *logger.Logger
	path    repos.PathRepo
	files   repos.MaterialFileRepo
	fileSigs repos.MaterialFileSignatureRepo
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	path repos.PathRepo,
	files repos.MaterialFileRepo,
	fileSigs repos.MaterialFileSignatureRepo,
) *Pipeline {
	return &Pipeline{
		db:      db,
		log:     baseLog.With("job", "path_grouping_refine"),
		path:    path,
		files:   files,
		fileSigs: fileSigs,
	}
}

func (p *Pipeline) Type() string { return "path_grouping_refine" }
