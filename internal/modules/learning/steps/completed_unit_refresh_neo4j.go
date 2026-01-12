package steps

import (
	"context"

	"github.com/google/uuid"

	graphstore "github.com/yungbote/neurobridge-backend/internal/data/graph"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

func syncUserConceptStatesToNeo4j(ctx context.Context, deps CompletedUnitRefreshDeps, userID uuid.UUID, materialSetID uuid.UUID) error {
	if deps.Graph == nil || deps.Graph.Driver == nil {
		return nil
	}
	if deps.Mastery == nil {
		return nil
	}
	if userID == uuid.Nil || materialSetID == uuid.Nil {
		return nil
	}

	pathID, err := deps.Bootstrap.EnsurePath(dbctx.Context{Ctx: ctx}, userID, materialSetID)
	if err != nil {
		return err
	}

	concepts, err := deps.Concepts.GetByScope(dbctx.Context{Ctx: ctx}, "path", &pathID)
	if err != nil {
		return err
	}
	conceptIDs := make([]uuid.UUID, 0, len(concepts))
	for _, c := range concepts {
		if c != nil && c.ID != uuid.Nil {
			conceptIDs = append(conceptIDs, c.ID)
		}
	}
	if len(conceptIDs) == 0 {
		return nil
	}

	rows, err := deps.Mastery.ListByUserAndConceptIDs(dbctx.Context{Ctx: ctx}, userID, conceptIDs)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}

	return graphstore.UpsertUserConceptStates(ctx, deps.Graph, deps.Log, userID, rows)
}
