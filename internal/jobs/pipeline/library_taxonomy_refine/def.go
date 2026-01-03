package library_taxonomy_refine

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/clients/openai"
	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type Pipeline struct {
	db  *gorm.DB
	log *logger.Logger
	ai  openai.Client

	path      repos.PathRepo
	pathNodes repos.PathNodeRepo
	clusters  repos.ConceptClusterRepo

	taxNodes    repos.LibraryTaxonomyNodeRepo
	taxEdges    repos.LibraryTaxonomyEdgeRepo
	membership  repos.LibraryTaxonomyMembershipRepo
	state       repos.LibraryTaxonomyStateRepo
	snapshots   repos.LibraryTaxonomySnapshotRepo
	pathVectors repos.LibraryPathEmbeddingRepo
}

func New(
	db *gorm.DB,
	baseLog *logger.Logger,
	ai openai.Client,
	path repos.PathRepo,
	pathNodes repos.PathNodeRepo,
	clusters repos.ConceptClusterRepo,
	taxNodes repos.LibraryTaxonomyNodeRepo,
	taxEdges repos.LibraryTaxonomyEdgeRepo,
	membership repos.LibraryTaxonomyMembershipRepo,
	state repos.LibraryTaxonomyStateRepo,
	snapshots repos.LibraryTaxonomySnapshotRepo,
	pathVectors repos.LibraryPathEmbeddingRepo,
) *Pipeline {
	return &Pipeline{
		db:          db,
		log:         baseLog.With("job", "library_taxonomy_refine"),
		ai:          ai,
		path:        path,
		pathNodes:   pathNodes,
		clusters:    clusters,
		taxNodes:    taxNodes,
		taxEdges:    taxEdges,
		membership:  membership,
		state:       state,
		snapshots:   snapshots,
		pathVectors: pathVectors,
	}
}

func (p *Pipeline) Type() string { return "library_taxonomy_refine" }

