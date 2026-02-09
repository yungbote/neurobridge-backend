package doc_variant_eval

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
	pathID, _ := jc.PayloadUUID("path_id")

	jc.Progress("evaluate", 2, "Evaluating doc variant outcomes")
	out, err := learningmod.New(learningmod.UsecasesDeps{
		DB:                  p.db,
		Log:                 p.log,
		DocVariantExposures: p.exposures,
		DocVariantOutcomes:  p.outcomes,
		NodeRuns:            p.nodeRuns,
		ConceptState:        p.conceptState,
		Bootstrap:           p.bootstrap,
	}).DocVariantEval(jc.Ctx, learningmod.DocVariantEvalInput{
		OwnerUserID:   jc.Job.OwnerUserID,
		MaterialSetID: setID,
		PathID:        pathID,
	})
	if err != nil {
		jc.Fail("evaluate", err)
		return nil
	}

	jc.Succeed("done", map[string]any{
		"path_id":          out.PathID.String(),
		"considered":       out.Considered,
		"outcomes_created": out.OutcomesCreated,
		"outcomes_skipped": out.OutcomesSkipped,
	})
	return nil
}
