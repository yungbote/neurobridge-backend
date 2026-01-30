package psu_build

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
	sagaID, _ := jc.PayloadUUID("saga_id")
	pathID, _ := jc.PayloadUUID("path_id")

	jc.Progress("psu_build", 2, "Building path structural units")
	out, err := learningmod.New(learningmod.UsecasesDeps{
		DB:                  p.db,
		Log:                 p.log,
		PathNodes:           p.nodes,
		Concepts:            p.concepts,
		PathStructuralUnits: p.psus,
		Bootstrap:           p.bootstrap,
	}).PathStructuralUnitBuild(jc.Ctx, learningmod.PathStructuralUnitBuildInput{
		OwnerUserID:   jc.Job.OwnerUserID,
		MaterialSetID: setID,
		SagaID:        sagaID,
		PathID:        pathID,
	})
	if err != nil {
		jc.Fail("psu_build", err)
		return nil
	}

	jc.Succeed("done", map[string]any{
		"material_set_id": setID.String(),
		"saga_id":         sagaID.String(),
		"path_id":         out.PathID.String(),
		"units":           out.Units,
	})
	return nil
}
