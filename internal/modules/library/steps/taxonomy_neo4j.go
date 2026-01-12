package steps

import (
	"context"

	"github.com/google/uuid"

	graphstore "github.com/yungbote/neurobridge-backend/internal/data/graph"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

func syncLibraryTaxonomyFacetToNeo4j(ctx context.Context, deps LibraryTaxonomyRouteDeps, userID uuid.UUID, facet string) error {
	if deps.Graph == nil || deps.Graph.Driver == nil {
		return nil
	}
	if userID == uuid.Nil {
		return nil
	}
	facet = normalizeFacet(facet)
	if facet == "" {
		return nil
	}

	nodes, err := deps.TaxNodes.GetByUserFacet(dbctx.Context{Ctx: ctx}, userID, facet)
	if err != nil {
		return err
	}
	edges, err := deps.TaxEdges.GetByUserFacet(dbctx.Context{Ctx: ctx}, userID, facet)
	if err != nil {
		return err
	}
	mems, err := deps.Membership.GetByUserFacet(dbctx.Context{Ctx: ctx}, userID, facet)
	if err != nil {
		return err
	}
	return graphstore.UpsertLibraryTaxonomyGraph(ctx, deps.Graph, deps.Log, userID, facet, nodes, edges, mems)
}
