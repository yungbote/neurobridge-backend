package chat_maintain

import (
	"fmt"

	"github.com/google/uuid"

	"github.com/yungbote/neurobridge-backend/internal/jobs/chat/steps"
	jobrt "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
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

	jc.Progress("maintain", 5, "Updating chat indexes")
	if err := steps.MaintainThread(jc.Ctx, steps.MaintainDeps{
		DB:       p.db,
		Log:      p.log,
		AI:       p.ai,
		Vec:      p.vec,
		Threads:  p.threads,
		Messages: p.messages,
		State:    p.state,
		Summaries: p.summaries,
		Docs:     p.docs,
		Memory:   p.memory,
		Entities: p.entities,
		Edges:    p.edges,
		Claims:   p.claims,
	}, steps.MaintainInput{
		UserID:   jc.Job.OwnerUserID,
		ThreadID: threadID,
	}); err != nil {
		jc.Fail("maintain", err)
		return nil
	}

	jc.Succeed("done", map[string]any{
		"thread_id": threadID.String(),
	})
	return nil
}

