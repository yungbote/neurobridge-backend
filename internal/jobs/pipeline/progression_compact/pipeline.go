package progression_compact

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

	jc.Progress("compact", 2, "Compacting progression events")
	out, err := learningmod.New(learningmod.UsecasesDeps{
		DB:               p.db,
		Log:              p.log,
		UserEvents:       p.events,
		UserEventCursors: p.cursors,
		ProgEvents:       p.progress,
		Bootstrap:        p.bootstrap,
	}).ProgressionCompact(jc.Ctx, learningmod.ProgressionCompactInput{
		OwnerUserID:   jc.Job.OwnerUserID,
		MaterialSetID: setID,
		SagaID:        sagaID,
		PathID:        pathID,
	})
	if err != nil {
		jc.Fail("compact", err)
		return nil
	}

	res := map[string]any{
		"material_set_id": setID.String(),
		"processed":       out.Processed,
	}
	if sagaID != uuid.Nil {
		res["saga_id"] = sagaID.String()
	}
	jc.Succeed("done", res)
	return nil
}
