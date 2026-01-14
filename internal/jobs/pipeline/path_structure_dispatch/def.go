package path_structure_dispatch

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type Pipeline struct {
	db   *gorm.DB
	log  *logger.Logger
	jobs services.JobService

	jobRuns          repos.JobRunRepo
	path             repos.PathRepo
	files            repos.MaterialFileRepo
	materialSets     repos.MaterialSetRepo
	materialSetFiles repos.MaterialSetFileRepo
	uli              repos.UserLibraryIndexRepo
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	jobs services.JobService,
	jobRuns repos.JobRunRepo,
	path repos.PathRepo,
	files repos.MaterialFileRepo,
	materialSets repos.MaterialSetRepo,
	materialSetFiles repos.MaterialSetFileRepo,
	uli repos.UserLibraryIndexRepo,
) *Pipeline {
	return &Pipeline{
		db:               db,
		log:              baseLog.With("job", "path_structure_dispatch"),
		jobs:             jobs,
		jobRuns:          jobRuns,
		path:             path,
		files:            files,
		materialSets:     materialSets,
		materialSetFiles: materialSetFiles,
		uli:              uli,
	}
}

func (p *Pipeline) Type() string { return "path_structure_dispatch" }
