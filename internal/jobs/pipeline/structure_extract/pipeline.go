package structure_extract

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
		ThreadState:  p.state,
		Concepts:     p.concepts,
		ConceptModel: p.model,
		MisconRepo:   p.miscon,
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

	jc.Succeed("done", map[string]any{
		"thread_id": threadID.String(),
		"processed": out.Processed,
		"max_seq":   out.MaxSeq,
	})
	return nil
}
