package doc_probe_select

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type Pipeline struct {
	db           *gorm.DB
	log          *logger.Logger
	path         repos.PathRepo
	pathRuns     repos.PathRunRepo
	nodes        repos.PathNodeRepo
	docs         repos.LearningNodeDocRepo
	docVariants  repos.LearningNodeDocVariantRepo
	concepts     repos.ConceptRepo
	conceptState repos.UserConceptStateRepo
	miscon       repos.UserMisconceptionInstanceRepo
	testlets     repos.UserTestletStateRepo
	docProbes    repos.DocProbeRepo
	bootstrap    services.LearningBuildBootstrapService
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	path repos.PathRepo,
	pathRuns repos.PathRunRepo,
	nodes repos.PathNodeRepo,
	docs repos.LearningNodeDocRepo,
	docVariants repos.LearningNodeDocVariantRepo,
	concepts repos.ConceptRepo,
	conceptState repos.UserConceptStateRepo,
	miscon repos.UserMisconceptionInstanceRepo,
	testlets repos.UserTestletStateRepo,
	docProbes repos.DocProbeRepo,
	bootstrap services.LearningBuildBootstrapService,
) *Pipeline {
	return &Pipeline{
		db:           db,
		log:          baseLog.With("job", "doc_probe_select"),
		path:         path,
		pathRuns:     pathRuns,
		nodes:        nodes,
		docs:         docs,
		docVariants:  docVariants,
		concepts:     concepts,
		conceptState: conceptState,
		miscon:       miscon,
		testlets:     testlets,
		docProbes:    docProbes,
		bootstrap:    bootstrap,
	}
}

func (p *Pipeline) Type() string { return "doc_probe_select" }
