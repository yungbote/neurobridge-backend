package coverage_coherence_audit

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
	pathID, _ := jc.PayloadUUID("path_id")

	jc.Progress("audit", 2, "Auditing coverage/coherence")
	out, err := learningmod.New(learningmod.UsecasesDeps{
		DB:         p.db,
		Log:        p.log,
		Path:       p.path,
		PathNodes:  p.nodes,
		Concepts:   p.concepts,
		Activities: p.activities,
		Variants:   p.variants,
		AI:         p.ai,
		Bootstrap:  p.bootstrap,
	}).CoverageCoherenceAudit(jc.Ctx, learningmod.CoverageCoherenceAuditInput{
		OwnerUserID:   jc.Job.OwnerUserID,
		MaterialSetID: setID,
		SagaID:        sagaID,
		PathID:        pathID,
	})
	if err != nil {
		jc.Fail("audit", err)
		return nil
	}

	jc.Succeed("done", map[string]any{
		"material_set_id":  setID.String(),
		"saga_id":          sagaID.String(),
		"audit_written":    out.AuditWritten,
		"acceptance":       out.Acceptance,
		"quality_warnings": out.QualityWarnings,
	})
	return nil
}
