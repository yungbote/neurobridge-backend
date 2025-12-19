package coverage_coherence_audit

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/clients/openai"
	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type Pipeline struct {
	db         *gorm.DB
	log        *logger.Logger
	path       repos.PathRepo
	nodes      repos.PathNodeRepo
	concepts   repos.ConceptRepo
	activities repos.ActivityRepo
	variants   repos.ActivityVariantRepo
	ai         openai.Client
	bootstrap  services.LearningBuildBootstrapService
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	path repos.PathRepo,
	nodes repos.PathNodeRepo,
	concepts repos.ConceptRepo,
	activities repos.ActivityRepo,
	variants repos.ActivityVariantRepo,
	ai openai.Client,
	bootstrap services.LearningBuildBootstrapService,
) *Pipeline {
	return &Pipeline{
		db:         db,
		log:        baseLog.With("job", "coverage_coherence_audit"),
		path:       path,
		nodes:      nodes,
		concepts:   concepts,
		activities: activities,
		variants:   variants,
		ai:         ai,
		bootstrap:  bootstrap,
	}
}

func (p *Pipeline) Type() string { return "coverage_coherence_audit" }
