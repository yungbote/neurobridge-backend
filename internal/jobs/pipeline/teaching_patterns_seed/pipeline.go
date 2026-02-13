package teaching_patterns_seed

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

	jc.Progress("patterns", 2, "Seeding teaching patterns")
	out, err := learningmod.New(learningmod.UsecasesDeps{
		DB:               p.db,
		Log:              p.log,
		TeachingPatterns: p.patterns,
		UserProfile:      p.profile,
		AI:               p.ai,
		Vec:              p.vec,
		Saga:             p.saga,
		Bootstrap:        p.bootstrap,
	}).TeachingPatternsSeed(jc.Ctx, learningmod.TeachingPatternsSeedInput{
		OwnerUserID:   jc.Job.OwnerUserID,
		MaterialSetID: setID,
		SagaID:        sagaID,
		PathID:        pathID,
	})
	if err != nil {
		jc.Fail("patterns", err)
		return nil
	}

	meta := map[string]any{
		"job_run_id":      jc.Job.ID.String(),
		"owner_user_id":   jc.Job.OwnerUserID.String(),
		"material_set_id": setID.String(),
		"path_id":         pathID.String(),
		"seeded":          out.Seeded,
	}
	inputs := map[string]any{
		"material_set_id": setID.String(),
		"saga_id":         sagaID.String(),
		"path_id":         pathID.String(),
	}
	chosen := map[string]any{
		"seeded": out.Seeded,
	}
	userID := jc.Job.OwnerUserID
	if _, traceErr := structuraltrace.Record(jc.Ctx, structuraltrace.Deps{DB: p.db, Log: p.log}, structuraltrace.TraceInput{
		DecisionType:  p.Type(),
		DecisionPhase: "build",
		DecisionMode:  "deterministic",
		UserID:        &userID,
		PathID:        &pathID,
		MaterialSetID: &setID,
		SagaID:        &sagaID,
		Inputs:        inputs,
		Chosen:        chosen,
		Metadata:      meta,
		Payload:       jc.Payload(),
		Validate:      true,
		RequireTrace:  true,
	}); traceErr != nil {
		jc.Fail("structural_trace", traceErr)
		return nil
	}

	jc.Succeed("done", map[string]any{
		"material_set_id": setID.String(),
		"saga_id":         sagaID.String(),
		"seeded":          out.Seeded,
	})
	return nil
}
