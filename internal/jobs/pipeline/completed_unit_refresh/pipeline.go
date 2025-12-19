package completed_unit_refresh

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

	jc.Progress("completed", 2, "Refreshing completed units")
	out, err := steps.CompletedUnitRefresh(jc.Ctx, steps.CompletedUnitRefreshDeps{
		DB:        p.db,
		Log:       p.log,
		Completed: p.completed,
		Progress:  p.progress,
		Concepts:  p.concepts,
		Act:       p.act,
		ActCon:    p.actCon,
		Chains:    p.chains,
		Mastery:   p.mastery,
		Bootstrap: p.bootstrap,
	}, steps.CompletedUnitRefreshInput{
		OwnerUserID:   jc.Job.OwnerUserID,
		MaterialSetID: setID,
		SagaID:        sagaID,
	})
	if err != nil {
		jc.Fail("completed", err)
		return nil
	}

	jc.Succeed("done", map[string]any{
		"material_set_id": setID.String(),
		"saga_id":         sagaID.String(),
		"noop":            out.Noop,
		"units_upserted":  out.UnitsUpserted,
		"units_completed": out.UnitsCompleted,
		"chains":          out.ChainsEvaluated,
	})
	return nil
}
