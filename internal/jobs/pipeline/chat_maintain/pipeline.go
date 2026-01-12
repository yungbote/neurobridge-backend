package chat_maintain

import (
	"fmt"

	"github.com/google/uuid"

	jobrt "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
	chatmod "github.com/yungbote/neurobridge-backend/internal/modules/chat"
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
	if err := chatmod.New(chatmod.UsecasesDeps{
		DB:        p.db,
		Log:       p.log,
		AI:        p.ai,
		Vec:       p.vec,
		Graph:     p.graph,
		Threads:   p.threads,
		Messages:  p.messages,
		State:     p.state,
		Summaries: p.summaries,
		Docs:      p.docs,
		Memory:    p.memory,
		Entities:  p.entities,
		Edges:     p.edges,
		Claims:    p.claims,
	}).MaintainThread(jc.Ctx, chatmod.MaintainInput{
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
