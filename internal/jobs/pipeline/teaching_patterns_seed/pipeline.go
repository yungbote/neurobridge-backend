package teaching_patterns_seed

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

	jc.Progress("patterns", 2, "Seeding teaching patterns")
	out, err := steps.TeachingPatternsSeed(jc.Ctx, steps.TeachingPatternsSeedDeps{
		DB:          p.db,
		Log:         p.log,
		Patterns:    p.patterns,
		UserProfile: p.profile,
		AI:          p.ai,
		Vec:         p.vec,
		Saga:        p.saga,
		Bootstrap:   p.bootstrap,
	}, steps.TeachingPatternsSeedInput{
		OwnerUserID:   jc.Job.OwnerUserID,
		MaterialSetID: setID,
		SagaID:        sagaID,
	})
	if err != nil {
		jc.Fail("patterns", err)
		return nil
	}

	jc.Succeed("done", map[string]any{
		"material_set_id": setID.String(),
		"saga_id":         sagaID.String(),
		"seeded":          out.Seeded,
	})
	return nil
}
