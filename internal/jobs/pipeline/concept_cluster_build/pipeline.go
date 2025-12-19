package concept_cluster_build

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

	jc.Progress("concept_clusters", 2, "Building concept clusters")
	out, err := steps.ConceptClusterBuild(jc.Ctx, steps.ConceptClusterBuildDeps{
		DB:        p.db,
		Log:       p.log,
		Concepts:  p.concepts,
		Clusters:  p.clusters,
		Members:   p.members,
		AI:        p.ai,
		Vec:       p.vec,
		Saga:      p.saga,
		Bootstrap: p.bootstrap,
	}, steps.ConceptClusterBuildInput{
		OwnerUserID:   jc.Job.OwnerUserID,
		MaterialSetID: setID,
		SagaID:        sagaID,
	})
	if err != nil {
		jc.Fail("concept_clusters", err)
		return nil
	}

	jc.Succeed("done", map[string]any{
		"material_set_id":  setID.String(),
		"saga_id":          sagaID.String(),
		"path_id":          out.PathID.String(),
		"clusters_made":    out.ClustersMade,
		"members_made":     out.MembersMade,
		"pinecone_batches": out.PineconeBatches,
	})
	return nil
}
