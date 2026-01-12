package steps

import (
	"context"

	"github.com/google/uuid"

	graphstore "github.com/yungbote/neurobridge-backend/internal/data/graph"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

func syncPathActivitiesToNeo4j(ctx context.Context, deps RealizeActivitiesDeps, pathID uuid.UUID) error {
	if deps.Graph == nil || deps.Graph.Driver == nil {
		return nil
	}
	if pathID == uuid.Nil {
		return nil
	}

	dbc := dbctx.Context{Ctx: ctx}
	pathRow, err := deps.Path.GetByID(dbc, pathID)
	if err != nil {
		return err
	}
	if pathRow == nil || pathRow.ID == uuid.Nil {
		return nil
	}

	nodes, err := deps.PathNodes.GetByPathIDs(dbc, []uuid.UUID{pathID})
	if err != nil {
		return err
	}

	activities, err := deps.Activities.ListByOwner(dbc, "path", &pathID)
	if err != nil {
		return err
	}
	activityIDs := make([]uuid.UUID, 0, len(activities))
	for _, a := range activities {
		if a != nil && a.ID != uuid.Nil {
			activityIDs = append(activityIDs, a.ID)
		}
	}

	variants, err := deps.Variants.GetByActivityIDs(dbc, activityIDs)
	if err != nil {
		return err
	}
	variantIDs := make([]uuid.UUID, 0, len(variants))
	for _, v := range variants {
		if v != nil && v.ID != uuid.Nil {
			variantIDs = append(variantIDs, v.ID)
		}
	}

	nodeIDs := make([]uuid.UUID, 0, len(nodes))
	for _, n := range nodes {
		if n != nil && n.ID != uuid.Nil {
			nodeIDs = append(nodeIDs, n.ID)
		}
	}
	nodeActs, err := deps.PathNodeActivities.GetByPathNodeIDs(dbc, nodeIDs)
	if err != nil {
		return err
	}

	actConcepts, err := deps.ActivityConcepts.GetByActivityIDs(dbc, activityIDs)
	if err != nil {
		return err
	}

	citations, err := deps.ActivityCitations.GetByActivityVariantIDs(dbc, variantIDs)
	if err != nil {
		return err
	}

	chunkIDs := make([]uuid.UUID, 0, len(citations))
	seenChunk := map[uuid.UUID]bool{}
	for _, c := range citations {
		if c == nil || c.MaterialChunkID == uuid.Nil {
			continue
		}
		if seenChunk[c.MaterialChunkID] {
			continue
		}
		seenChunk[c.MaterialChunkID] = true
		chunkIDs = append(chunkIDs, c.MaterialChunkID)
	}
	chunks, err := deps.Chunks.GetByIDs(dbc, chunkIDs)
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
	files, err := deps.Files.GetByIDs(dbc, fileIDs)
	if err != nil {
		return err
	}

	return graphstore.UpsertPathActivitiesGraph(
		ctx,
		deps.Graph,
		deps.Log,
		pathRow,
		nodes,
		activities,
		variants,
		nodeActs,
		actConcepts,
		citations,
		chunks,
		files,
	)
}
