package realize_activities

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

	jc.Progress("realize", 2, "Creating activities")
	out, err := steps.RealizeActivities(jc.Ctx, steps.RealizeActivitiesDeps{
		DB:                 p.db,
		Log:                p.log,
		Path:               p.path,
		PathNodes:          p.nodes,
		PathNodeActivities: p.nodeActivities,
		Activities:         p.activities,
		Variants:           p.variants,
		ActivityConcepts:   p.activityConcepts,
		ActivityCitations:  p.activityCites,
		Concepts:           p.concepts,
		Files:              p.files,
		Chunks:             p.chunks,
		UserProfile:        p.profile,
		AI:                 p.ai,
		Vec:                p.vec,
		Saga:               p.saga,
		Bootstrap:          p.bootstrap,
	}, steps.RealizeActivitiesInput{
		OwnerUserID:   jc.Job.OwnerUserID,
		MaterialSetID: setID,
		SagaID:        sagaID,
	})
	if err != nil {
		jc.Fail("realize", err)
		return nil
	}

	jc.Succeed("done", map[string]any{
		"material_set_id": setID.String(),
		"saga_id":         sagaID.String(),
		"path_id":         out.PathID.String(),
		"activities_made": out.ActivitiesMade,
		"variants_made":   out.VariantsMade,
	})
	return nil
}
