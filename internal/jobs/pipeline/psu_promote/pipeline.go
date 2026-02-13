package psu_promote

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

	meta := map[string]any{
		"job_run_id":    jc.Job.ID.String(),
		"owner_user_id": jc.Job.OwnerUserID.String(),
		"user_id":       userID.String(),
		"candidates":    out.Candidates,
		"considered":    out.Considered,
		"promoted":      out.Promoted,
		"demoted":       out.Demoted,
	}
	inputs := map[string]any{
		"user_id": userID.String(),
	}
	chosen := map[string]any{
		"promoted": out.Promoted,
		"demoted":  out.Demoted,
	}
	_, traceErr := structuraltrace.Record(jc.Ctx, structuraltrace.Deps{DB: p.db, Log: p.log}, structuraltrace.TraceInput{
		DecisionType:  p.Type(),
		DecisionPhase: "build",
		DecisionMode:  "deterministic",
		UserID:        &userID,
		Inputs:        inputs,
		Chosen:        chosen,
		Metadata:      meta,
		Payload:       jc.Payload(),
		Validate:      false,
		RequireTrace:  true,
	})
	if traceErr != nil {
		jc.Fail("structural_trace", traceErr)
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
