package steps

import (
	"context"

	"github.com/google/uuid"

	graphstore "github.com/yungbote/neurobridge-backend/internal/data/graph"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

func syncUserConceptStatesToNeo4j(ctx context.Context, deps CompletedUnitRefreshDeps, userID uuid.UUID, pathID uuid.UUID) error {
	if deps.Graph == nil || deps.Graph.Driver == nil {
		return nil
	}
	if deps.Mastery == nil {
		return nil
	}
	if userID == uuid.Nil {
		return nil
	}
	if deps.Concepts == nil {
		return nil
	}

	if pathID == uuid.Nil {
		return nil
	}

	concepts, err := deps.Concepts.GetByScope(dbctx.Context{Ctx: ctx}, "path", &pathID)
	if err != nil {
		return err
	}
	conceptIDs := make([]uuid.UUID, 0, len(concepts))
	seen := map[uuid.UUID]bool{}
	for _, c := range concepts {
		if c == nil || c.ID == uuid.Nil {
			continue
		}
		id := c.ID
		if c.CanonicalConceptID != nil && *c.CanonicalConceptID != uuid.Nil {
			id = *c.CanonicalConceptID
		}
		if id == uuid.Nil || seen[id] {
			continue
		}
		seen[id] = true
		conceptIDs = append(conceptIDs, id)
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
