package steps

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"

	graphstore "github.com/yungbote/neurobridge-backend/internal/data/graph"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

func syncPathStructureToNeo4j(ctx context.Context, deps PathPlanBuildDeps, pathID uuid.UUID) error {
	if deps.Graph == nil || deps.Graph.Driver == nil {
		return nil
	}
	if pathID == uuid.Nil {
		return nil
	}

	pathRow, err := deps.Path.GetByID(dbctx.Context{Ctx: ctx}, pathID)
	if err != nil {
		return err
	}
	if pathRow == nil || pathRow.ID == uuid.Nil {
		return nil
	}

	nodes, err := deps.PathNodes.GetByPathIDs(dbctx.Context{Ctx: ctx}, []uuid.UUID{pathID})
	if err != nil {
		return err
	}

	concepts, err := deps.Concepts.GetByScope(dbctx.Context{Ctx: ctx}, "path", &pathID)
	if err != nil {
		return err
	}
	keyToID := map[string]uuid.UUID{}
	for _, c := range concepts {
		if c == nil || c.ID == uuid.Nil {
			continue
		}
		k := strings.ToLower(strings.TrimSpace(c.Key))
		if k != "" {
			keyToID[k] = c.ID
		}
	}

	edges := make([]graphstore.PathNodeConceptEdge, 0)
	for _, n := range nodes {
		if n == nil || n.ID == uuid.Nil {
			continue
		}
		var meta map[string]any
		if len(n.Metadata) > 0 && strings.TrimSpace(string(n.Metadata)) != "" && strings.TrimSpace(string(n.Metadata)) != "null" {
			_ = json.Unmarshal(n.Metadata, &meta)
		}

		covers := dedupeStrings(stringSliceFromAny(meta["concept_keys"]))
		for _, k := range covers {
			id := keyToID[strings.ToLower(strings.TrimSpace(k))]
			if id == uuid.Nil {
				continue
			}
			edges = append(edges, graphstore.PathNodeConceptEdge{
				PathNodeID: n.ID,
				ConceptID:  id,
				Kind:       "covers",
				Weight:     1,
			})
		}

		req := dedupeStrings(stringSliceFromAny(meta["prereq_concept_keys"]))
		for _, k := range req {
			id := keyToID[strings.ToLower(strings.TrimSpace(k))]
			if id == uuid.Nil {
				continue
			}
			edges = append(edges, graphstore.PathNodeConceptEdge{
				PathNodeID: n.ID,
				ConceptID:  id,
				Kind:       "requires",
				Weight:     1,
			})
		}
	}

	return graphstore.UpsertPathStructureGraph(ctx, deps.Graph, deps.Log, pathRow, nodes, edges)
}
