package concept_graph_build

import (
	"fmt"

	"github.com/google/uuid"

	"github.com/yungbote/neurobridge-backend/internal/jobs/learning/steps"
	jobrt "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
)

func (p *Pipeline) Run(jc *jobrt.Context) error {
	if jc == nil || jc.Job == nil {
		return nil
	}
	setID, ok := jc.PayloadUUID("material_set_id")
	if !ok || setID == uuid.Nil {
		jc.Fail("validate", fmt.Errorf("missing material_set_id"))
		return nil
	}
	sagaID, ok := jc.PayloadUUID("saga_id")
	if !ok || sagaID == uuid.Nil {
		jc.Fail("validate", fmt.Errorf("missing saga_id"))
		return nil
	}

	jc.Progress("concept_graph", 2, "Building concept graph")
	out, err := steps.ConceptGraphBuild(jc.Ctx, steps.ConceptGraphBuildDeps{
		DB:        p.db,
		Log:       p.log,
		Files:     p.files,
		Chunks:    p.chunks,
		Concepts:  p.concepts,
		Evidence:  p.evidence,
		Edges:     p.edges,
		AI:        p.ai,
		Vec:       p.vec,
		Saga:      p.saga,
		Bootstrap: p.bootstrap,
	}, steps.ConceptGraphBuildInput{
		OwnerUserID:   jc.Job.OwnerUserID,
		MaterialSetID: setID,
		SagaID:        sagaID,
	})
	if err != nil {
		jc.Fail("concept_graph", err)
		return nil
	}

	jc.Succeed("done", map[string]any{
		"material_set_id":  setID.String(),
		"saga_id":          sagaID.String(),
		"path_id":          out.PathID.String(),
		"concepts_made":    out.ConceptsMade,
		"edges_made":       out.EdgesMade,
		"pinecone_batches": out.PineconeBatches,
	})
	return nil
}
