package priors_refresh

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

	jc.Progress("priors", 2, "Refreshing priors")
	out, err := steps.PriorsRefresh(jc.Ctx, steps.PriorsRefreshDeps{
		DB:           p.db,
		Log:          p.log,
		Activities:   p.activities,
		Variants:     p.variants,
		VariantStats: p.stats,
		Chains:       p.chains,
		Concepts:     p.concepts,
		ActConcepts:  p.actConcept,
		ChainPriors:  p.chain,
		CohortPriors: p.cohort,
		Bootstrap:    p.bootstrap,
	}, steps.PriorsRefreshInput{
		OwnerUserID:   jc.Job.OwnerUserID,
		MaterialSetID: setID,
		SagaID:        sagaID,
	})
	if err != nil {
		jc.Fail("priors", err)
		return nil
	}

	jc.Succeed("done", map[string]any{
		"material_set_id": setID.String(),
		"saga_id":         sagaID.String(),
		"noop":            out.Noop,
		"variants":        out.VariantsConsidered,
		"chain_priors":    out.ChainPriorsUpserted,
		"cohort_priors":   out.CohortPriorsUpserted,
	})
	return nil
}
