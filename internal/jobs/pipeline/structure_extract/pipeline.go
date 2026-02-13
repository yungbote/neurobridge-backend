package structure_extract

import (
	"fmt"

	"github.com/google/uuid"

	"github.com/yungbote/neurobridge-backend/internal/jobs/pipeline/structuraltrace"
	jobrt "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
	learningmod "github.com/yungbote/neurobridge-backend/internal/modules/learning"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

func (p *Pipeline) Run(jc *jobrt.Context) error {
	if jc == nil || jc.Job == nil {
		return nil
	}
	threadID, ok := jc.PayloadUUID("thread_id")
	if !ok || threadID == uuid.Nil {
		jc.Fail("validate", fmt.Errorf("missing thread_id"))
		return nil
	}
	msgID, _ := jc.PayloadUUID("message_id")

	jc.Progress("structure_extract", 2, "Extracting learning structure")
	out, err := learningmod.New(learningmod.UsecasesDeps{
		DB:           p.db,
		Log:          p.log,
		Threads:      p.threads,
		Messages:     p.messages,
		Turns:        p.turns,
		ThreadState:  p.state,
		Concepts:     p.concepts,
		ConceptModel: p.model,
		MisconRepo:   p.miscon,
		UserEvents:   p.events,
		AI:           p.ai,
	}).StructureExtract(jc.Ctx, learningmod.StructureExtractInput{
		UserID:    jc.Job.OwnerUserID,
		ThreadID:  threadID,
		MessageID: msgID,
	})
	if err != nil {
		jc.Fail("structure_extract", err)
		return nil
	}

	meta := map[string]any{
		"job_run_id":    jc.Job.ID.String(),
		"owner_user_id": jc.Job.OwnerUserID.String(),
		"thread_id":     threadID.String(),
		"processed":     out.Processed,
		"max_seq":       out.MaxSeq,
	}
	inputs := map[string]any{
		"thread_id":  threadID.String(),
		"message_id": msgID.String(),
	}
	chosen := map[string]any{
		"processed": out.Processed,
		"max_seq":   out.MaxSeq,
	}
	userID := jc.Job.OwnerUserID
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
		"thread_id": threadID.String(),
		"processed": out.Processed,
		"max_seq":   out.MaxSeq,
	})
	if out.Processed > 0 && p.jobs != nil {
		dbc := dbctx.Context{Ctx: jc.Ctx}
		_, _, _ = p.jobs.EnqueueRuntimeUpdateIfNeeded(dbc, jc.Job.OwnerUserID, "structure_extract")
	}
	return nil
}
