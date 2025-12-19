package path_plan_build

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

	jc.Progress("path_plan", 2, "Building path plan")
	out, err := steps.PathPlanBuild(jc.Ctx, steps.PathPlanBuildDeps{
		DB:          p.db,
		Log:         p.log,
		Path:        p.path,
		PathNodes:   p.nodes,
		Concepts:    p.concepts,
		Edges:       p.edges,
		Summaries:   p.summaries,
		UserProfile: p.profile,
		AI:          p.ai,
		Bootstrap:   p.bootstrap,
	}, steps.PathPlanBuildInput{
		OwnerUserID:   jc.Job.OwnerUserID,
		MaterialSetID: setID,
		SagaID:        sagaID,
	})
	if err != nil {
		jc.Fail("path_plan", err)
		return nil
	}

	jc.Succeed("done", map[string]any{
		"material_set_id": setID.String(),
		"saga_id":         sagaID.String(),
		"path_id":         out.PathID.String(),
		"nodes":           out.Nodes,
	})
	return nil
}
