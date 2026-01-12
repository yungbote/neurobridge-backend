package library

import (
	"context"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/modules/library/steps"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/platform/neo4jdb"
	"github.com/yungbote/neurobridge-backend/internal/platform/openai"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type UsecasesDeps struct {
	DB  *gorm.DB
	Log *logger.Logger

	AI openai.Client
	// Optional: sync computed taxonomy into Neo4j for fast traversals.
	Graph *neo4jdb.Client

	Path        repos.PathRepo
	PathNodes   repos.PathNodeRepo
	Clusters    repos.ConceptClusterRepo
	TaxNodes    repos.LibraryTaxonomyNodeRepo
	TaxEdges    repos.LibraryTaxonomyEdgeRepo
	Membership  repos.LibraryTaxonomyMembershipRepo
	State       repos.LibraryTaxonomyStateRepo
	Snapshots   repos.LibraryTaxonomySnapshotRepo
	PathVectors repos.LibraryPathEmbeddingRepo

	JobRuns repos.JobRunRepo
	Jobs    services.JobService
}

type Usecases struct {
	deps UsecasesDeps
}

func New(deps UsecasesDeps) Usecases { return Usecases{deps: deps} }

func (u Usecases) WithLog(log *logger.Logger) Usecases {
	u.deps.Log = log
	return u
}

type (
	LibraryTaxonomyRouteInput  = steps.LibraryTaxonomyRouteInput
	LibraryTaxonomyRouteOutput = steps.LibraryTaxonomyRouteOutput

	LibraryTaxonomyRefineInput  = steps.LibraryTaxonomyRefineInput
	LibraryTaxonomyRefineOutput = steps.LibraryTaxonomyRefineOutput
)

func (u Usecases) LibraryTaxonomyRoute(ctx context.Context, in LibraryTaxonomyRouteInput) (LibraryTaxonomyRouteOutput, error) {
	return steps.LibraryTaxonomyRoute(ctx, steps.LibraryTaxonomyRouteDeps{
		DB:          u.deps.DB,
		Log:         u.deps.Log,
		AI:          u.deps.AI,
		Graph:       u.deps.Graph,
		Path:        u.deps.Path,
		PathNodes:   u.deps.PathNodes,
		Clusters:    u.deps.Clusters,
		TaxNodes:    u.deps.TaxNodes,
		TaxEdges:    u.deps.TaxEdges,
		Membership:  u.deps.Membership,
		State:       u.deps.State,
		Snapshots:   u.deps.Snapshots,
		PathVectors: u.deps.PathVectors,
	}, steps.LibraryTaxonomyRouteInput(in))
}

func (u Usecases) LibraryTaxonomyRefine(ctx context.Context, in LibraryTaxonomyRefineInput) (LibraryTaxonomyRefineOutput, error) {
	return steps.LibraryTaxonomyRefine(ctx, steps.LibraryTaxonomyRouteDeps{
		DB:          u.deps.DB,
		Log:         u.deps.Log,
		AI:          u.deps.AI,
		Graph:       u.deps.Graph,
		Path:        u.deps.Path,
		PathNodes:   u.deps.PathNodes,
		Clusters:    u.deps.Clusters,
		TaxNodes:    u.deps.TaxNodes,
		TaxEdges:    u.deps.TaxEdges,
		Membership:  u.deps.Membership,
		State:       u.deps.State,
		Snapshots:   u.deps.Snapshots,
		PathVectors: u.deps.PathVectors,
	}, steps.LibraryTaxonomyRefineInput(in))
}

func (u Usecases) BuildAndPersistTaxonomySnapshot(ctx context.Context, userID uuid.UUID) error {
	return steps.BuildAndPersistLibraryTaxonomySnapshot(ctx, steps.LibraryTaxonomyRouteDeps{
		DB:          u.deps.DB,
		Log:         u.deps.Log,
		AI:          u.deps.AI,
		Graph:       u.deps.Graph,
		Path:        u.deps.Path,
		PathNodes:   u.deps.PathNodes,
		Clusters:    u.deps.Clusters,
		TaxNodes:    u.deps.TaxNodes,
		TaxEdges:    u.deps.TaxEdges,
		Membership:  u.deps.Membership,
		State:       u.deps.State,
		Snapshots:   u.deps.Snapshots,
		PathVectors: u.deps.PathVectors,
	}, userID)
}
