package library_taxonomy_refine

import (
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/neo4jdb"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
)

type Pipeline struct {
	db    *gorm.DB
	log   *logger.Logger
	ai    openai.Client
	graph *neo4jdb.Client

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
	graph *neo4jdb.Client,
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
		graph:       graph,
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
