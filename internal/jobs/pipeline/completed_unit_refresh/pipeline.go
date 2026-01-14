package completed_unit_refresh

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

	jc.Progress("completed", 2, "Refreshing completed units")
	out, err := learningmod.New(learningmod.UsecasesDeps{
		DB:               p.db,
		Log:              p.log,
		CompletedUnits:   p.completed,
		ProgEvents:       p.progress,
		Concepts:         p.concepts,
		Activities:       p.act,
		ActivityConcepts: p.actCon,
		ChainSignatures:  p.chains,
		ConceptState:     p.mastery,
		Graph:            p.graph,
		Bootstrap:        p.bootstrap,
	}).CompletedUnitRefresh(jc.Ctx, learningmod.CompletedUnitRefreshInput{
		OwnerUserID:   jc.Job.OwnerUserID,
		MaterialSetID: setID,
		SagaID:        sagaID,
		PathID:        pathID,
	})
	if err != nil {
		jc.Fail("completed", err)
		return nil
	}

	res := map[string]any{
		"material_set_id": setID.String(),
		"noop":            out.Noop,
		"units_upserted":  out.UnitsUpserted,
		"units_completed": out.UnitsCompleted,
		"chains":          out.ChainsEvaluated,
	}
	if sagaID != uuid.Nil {
		res["saga_id"] = sagaID.String()
	}
	jc.Succeed("done", res)
	return nil
}
