package aggregates

import (
	"context"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	domainagg "github.com/yungbote/neurobridge-backend/internal/domain/aggregates"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

type TaxonomyAggregateDeps struct {
	Base BaseDeps

	Nodes       repos.LibraryTaxonomyNodeRepo
	Edges       repos.LibraryTaxonomyEdgeRepo
	Memberships repos.LibraryTaxonomyMembershipRepo
	State       repos.LibraryTaxonomyStateRepo
	Snapshots   repos.LibraryTaxonomySnapshotRepo
}

type taxonomyAggregate struct {
	deps TaxonomyAggregateDeps
}

func NewTaxonomyAggregate(deps TaxonomyAggregateDeps) domainagg.TaxonomyAggregate {
	deps.Base = deps.Base.withDefaults()
	return &taxonomyAggregate{deps: deps}
}

func (a *taxonomyAggregate) Contract() domainagg.Contract {
	return domainagg.TaxonomyAggregateContract
}

func (a *taxonomyAggregate) ApplyTaxonomyRefinement(ctx context.Context, in domainagg.ApplyTaxonomyRefinementInput) (domainagg.ApplyTaxonomyRefinementResult, error) {
	const op = "Library.Taxonomy.ApplyTaxonomyRefinement"
	var out domainagg.ApplyTaxonomyRefinementResult
	err := executeWrite(ctx, a.deps.Base, op, func(_ dbctx.Context) error {
		return notImplemented(op)
	})
	return out, err
}

func (a *taxonomyAggregate) CommitTaxonomySnapshot(ctx context.Context, in domainagg.CommitTaxonomySnapshotInput) (domainagg.CommitTaxonomySnapshotResult, error) {
	const op = "Library.Taxonomy.CommitTaxonomySnapshot"
	var out domainagg.CommitTaxonomySnapshotResult
	err := executeWrite(ctx, a.deps.Base, op, func(_ dbctx.Context) error {
		return notImplemented(op)
	})
	return out, err
}
