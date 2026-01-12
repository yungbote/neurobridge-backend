package steps

import (
	"context"

	"github.com/google/uuid"

	graphstore "github.com/yungbote/neurobridge-backend/internal/data/graph"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

func syncPathConceptGraphToNeo4j(ctx context.Context, deps ConceptGraphBuildDeps, pathID uuid.UUID) error {
	if deps.Graph == nil || deps.Graph.Driver == nil {
		return nil
	}

	concepts, err := deps.Concepts.GetByScope(dbctx.Context{Ctx: ctx}, "path", &pathID)
	if err != nil {
		return err
	}
	if len(concepts) == 0 {
		return nil
	}

	ids := make([]uuid.UUID, 0, len(concepts))
	idSet := make(map[uuid.UUID]bool, len(concepts))
	for _, cc := range concepts {
		if cc == nil || cc.ID == uuid.Nil {
			continue
		}
		ids = append(ids, cc.ID)
		idSet[cc.ID] = true
	}

	edges, err := deps.Edges.GetByConceptIDs(dbctx.Context{Ctx: ctx}, ids)
	if err != nil {
		return err
	}
	filtered := make([]*types.ConceptEdge, 0, len(edges))
	for _, e := range edges {
		if e == nil {
			continue
		}
		if !idSet[e.FromConceptID] || !idSet[e.ToConceptID] {
			continue
		}
		filtered = append(filtered, e)
	}

	if err := graphstore.UpsertPathConceptGraph(ctx, deps.Graph, deps.Log, pathID, concepts, filtered); err != nil {
		return err
	}

	// Also sync concept evidence -> chunks/files for provenance/explainability.
	evidence, err := deps.Evidence.GetByConceptIDs(dbctx.Context{Ctx: ctx}, ids)
	if err != nil {
		return err
	}
	if len(evidence) == 0 {
		return nil
	}

	chunkIDs := make([]uuid.UUID, 0, len(evidence))
	seenChunk := map[uuid.UUID]bool{}
	for _, ev := range evidence {
		if ev == nil || ev.MaterialChunkID == uuid.Nil {
			continue
		}
		if seenChunk[ev.MaterialChunkID] {
			continue
		}
		seenChunk[ev.MaterialChunkID] = true
		chunkIDs = append(chunkIDs, ev.MaterialChunkID)
	}
	chunks, err := deps.Chunks.GetByIDs(dbctx.Context{Ctx: ctx}, chunkIDs)
	if err != nil {
		return err
	}

	fileIDs := make([]uuid.UUID, 0)
	seenFile := map[uuid.UUID]bool{}
	for _, ch := range chunks {
		if ch == nil || ch.MaterialFileID == uuid.Nil {
			continue
		}
		if seenFile[ch.MaterialFileID] {
			continue
		}
		seenFile[ch.MaterialFileID] = true
		fileIDs = append(fileIDs, ch.MaterialFileID)
	}
	files, err := deps.Files.GetByIDs(dbctx.Context{Ctx: ctx}, fileIDs)
	if err != nil {
		return err
	}

	return graphstore.UpsertConceptEvidenceGraph(ctx, deps.Graph, deps.Log, evidence, chunks, files)
}
