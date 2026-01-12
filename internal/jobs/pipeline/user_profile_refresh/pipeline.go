package user_profile_refresh

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

	jc.Progress("user_profile", 2, "Refreshing user profile")
	out, err := learningmod.New(learningmod.UsecasesDeps{
		DB:          p.db,
		Log:         p.log,
		StylePrefs:  p.stylePrefs,
		ProgEvents:  p.progEvents,
		UserProfile: p.profile,
		Prefs:       p.prefs,
		AI:          p.ai,
		Vec:         p.vec,
		Saga:        p.saga,
		Bootstrap:   p.bootstrap,
	}).UserProfileRefresh(jc.Ctx, learningmod.UserProfileRefreshInput{
		OwnerUserID:   jc.Job.OwnerUserID,
		MaterialSetID: setID,
		SagaID:        sagaID,
	})
	if err != nil {
		jc.Fail("user_profile", err)
		return nil
	}

	jc.Succeed("done", map[string]any{
		"material_set_id": setID.String(),
		"saga_id":         sagaID.String(),
		"vector_id":       out.VectorID,
	})
	return nil
}
