package priors_refresh

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
	// saga_id is optional for event-driven refresh runs (legacy learning_build always provided it).
	sagaID, _ := jc.PayloadUUID("saga_id")
	pathID, _ := jc.PayloadUUID("path_id")

	jc.Progress("priors", 2, "Refreshing priors")
	out, err := learningmod.New(learningmod.UsecasesDeps{
		DB:               p.db,
		Log:              p.log,
		Activities:       p.activities,
		Variants:         p.variants,
		VariantStats:     p.stats,
		ChainSignatures:  p.chains,
		Concepts:         p.concepts,
		ActivityConcepts: p.actConcept,
		ChainPriors:      p.chain,
		CohortPriors:     p.cohort,
		Bootstrap:        p.bootstrap,
	}).PriorsRefresh(jc.Ctx, learningmod.PriorsRefreshInput{
		OwnerUserID:   jc.Job.OwnerUserID,
		MaterialSetID: setID,
		SagaID:        sagaID,
		PathID:        pathID,
	})
	if err != nil {
		jc.Fail("priors", err)
		return nil
	}

	res := map[string]any{
		"material_set_id": setID.String(),
		"noop":            out.Noop,
		"variants":        out.VariantsConsidered,
		"chain_priors":    out.ChainPriorsUpserted,
		"cohort_priors":   out.CohortPriorsUpserted,
	}
	if sagaID != uuid.Nil {
		res["saga_id"] = sagaID.String()
	}
	jc.Succeed("done", res)
	return nil
}
