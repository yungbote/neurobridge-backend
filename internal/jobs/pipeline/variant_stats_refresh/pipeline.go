package variant_stats_refresh

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

	jc.Progress("variant_stats", 2, "Refreshing variant stats")
	out, err := steps.VariantStatsRefresh(jc.Ctx, steps.VariantStatsRefreshDeps{
		DB:        p.db,
		Log:       p.log,
		Events:    p.events,
		Cursors:   p.cursors,
		Variants:  p.variants,
		Stats:     p.stats,
		Bootstrap: p.bootstrap,
	}, steps.VariantStatsRefreshInput{
		OwnerUserID:   jc.Job.OwnerUserID,
		MaterialSetID: setID,
		SagaID:        sagaID,
	})
	if err != nil {
		jc.Fail("variant_stats", err)
		return nil
	}

	jc.Succeed("done", map[string]any{
		"material_set_id": setID.String(),
		"saga_id":         sagaID.String(),
		"processed":       out.Processed,
		"updated":         out.Updated,
	})
	return nil
}
