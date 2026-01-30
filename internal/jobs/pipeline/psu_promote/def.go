package psu_promote

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
)

type Pipeline struct {
	db           *gorm.DB
	log          *logger.Logger
	events       repos.UserEventRepo
	psus         repos.PathStructuralUnitRepo
	concepts     repos.ConceptRepo
	edges        repos.ConceptEdgeRepo
	conceptState repos.UserConceptStateRepo
	conceptModel repos.UserConceptModelRepo
	misconRepo   repos.UserMisconceptionInstanceRepo
	ai           openai.Client
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	events repos.UserEventRepo,
	psus repos.PathStructuralUnitRepo,
	concepts repos.ConceptRepo,
	edges repos.ConceptEdgeRepo,
	conceptState repos.UserConceptStateRepo,
	conceptModel repos.UserConceptModelRepo,
	misconRepo repos.UserMisconceptionInstanceRepo,
	ai openai.Client,
) *Pipeline {
	return &Pipeline{
		db:           db,
		log:          baseLog.With("job", "psu_promote"),
		events:       events,
		psus:         psus,
		concepts:     concepts,
		edges:        edges,
		conceptState: conceptState,
		conceptModel: conceptModel,
		misconRepo:   misconRepo,
		ai:           ai,
	}
}

func (p *Pipeline) Type() string { return "psu_promote" }
