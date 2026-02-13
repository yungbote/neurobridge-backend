package path_plan_build

import (
	"fmt"

	"github.com/google/uuid"

	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/structuraltrace"
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
	pathID, _ := jc.PayloadUUID("path_id")

	jc.Progress("path_plan", 2, "Building path plan")
	out, err := learningmod.New(learningmod.UsecasesDeps{
		DB:           p.db,
		Log:          p.log,
		Path:         p.path,
		PathNodes:    p.nodes,
		Concepts:     p.concepts,
		ConceptReps:  p.reps,
		Edges:        p.edges,
		Summaries:    p.summaries,
		UserProfile:  p.profile,
		ConceptState: p.mastery,
		ConceptModel: p.model,
		MisconRepo:   p.miscon,
		Graph:        p.graph,
		AI:           p.ai,
		Bootstrap:    p.bootstrap,
	}).PathPlanBuild(jc.Ctx, learningmod.PathPlanBuildInput{
		OwnerUserID:   jc.Job.OwnerUserID,
		MaterialSetID: setID,
		SagaID:        sagaID,
		PathID:        pathID,
	})
	if err != nil {
		jc.Fail("path_plan", err)
		return nil
	}

	meta := map[string]any{
		"job_run_id":      jc.Job.ID.String(),
		"owner_user_id":   jc.Job.OwnerUserID.String(),
		"material_set_id": setID.String(),
		"path_id":         out.PathID.String(),
		"nodes":           out.Nodes,
	}
	inputs := map[string]any{
		"material_set_id": setID.String(),
		"saga_id":         sagaID.String(),
		"path_id":         out.PathID.String(),
	}
	chosen := map[string]any{
		"nodes": out.Nodes,
	}
	userID := jc.Job.OwnerUserID
	_, traceErr := structuraltrace.Record(jc.Ctx, structuraltrace.Deps{DB: p.db, Log: p.log}, structuraltrace.TraceInput{
		DecisionType:  p.Type(),
		DecisionPhase: "build",
		DecisionMode:  "deterministic",
		UserID:        &userID,
		PathID:        &out.PathID,
		MaterialSetID: &setID,
		SagaID:        &sagaID,
		Inputs:        inputs,
		Chosen:        chosen,
		Metadata:      meta,
		Payload:       jc.Payload(),
		Validate:      true,
		RequireTrace:  true,
	})
	if traceErr != nil {
		jc.Fail("invariant_validation", traceErr)
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
