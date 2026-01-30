package psu_promote

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
	userID := jc.Job.OwnerUserID
	if raw, ok := jc.PayloadUUID("user_id"); ok && raw != uuid.Nil {
		userID = raw
	}
	if userID == uuid.Nil {
		jc.Fail("validate", fmt.Errorf("missing user_id"))
		return nil
	}

	jc.Progress("promote", 5, "Evaluating PSU promotions")
	out, err := learningmod.New(learningmod.UsecasesDeps{
		DB:                  p.db,
		Log:                 p.log,
		UserEvents:          p.events,
		PathStructuralUnits: p.psus,
		Concepts:            p.concepts,
		Edges:               p.edges,
		ConceptState:        p.conceptState,
		ConceptModel:        p.conceptModel,
		MisconRepo:          p.misconRepo,
		AI:                  p.ai,
	}).PSUPromotion(jc.Ctx, learningmod.PSUPromotionInput{
		UserID: userID,
	})
	if err != nil {
		jc.Fail("promote", err)
		return nil
	}

	jc.Succeed("done", map[string]any{
		"user_id":    userID.String(),
		"candidates": out.Candidates,
		"considered": out.Considered,
		"promoted":   out.Promoted,
		"demoted":    out.Demoted,
	})
	return nil
}
