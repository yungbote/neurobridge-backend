package chain_signature_build

import (
	"fmt"

	"github.com/google/uuid"

	jobrt "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
	learningmod "github.com/yungbote/neurobridge-backend/internal/modules/learning"
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

	jc.Progress("chain_signatures", 2, "Building chain signatures")
	out, err := learningmod.New(learningmod.UsecasesDeps{
		DB:              p.db,
		Log:             p.log,
		Concepts:        p.concepts,
		Clusters:        p.clusters,
		Members:         p.members,
		Edges:           p.edges,
		ChainSignatures: p.chains,
		AI:              p.ai,
		Vec:             p.vec,
		Saga:            p.saga,
		Bootstrap:       p.bootstrap,
	}).ChainSignatureBuild(jc.Ctx, learningmod.ChainSignatureBuildInput{
		OwnerUserID:   jc.Job.OwnerUserID,
		MaterialSetID: setID,
		SagaID:        sagaID,
	})
	if err != nil {
		jc.Fail("chain_signatures", err)
		return nil
	}

	jc.Succeed("done", map[string]any{
		"material_set_id":  setID.String(),
		"saga_id":          sagaID.String(),
		"path_id":          out.PathID.String(),
		"chains_upserted":  out.ChainsUpserted,
		"pinecone_batches": out.PineconeBatches,
	})
	return nil
}
