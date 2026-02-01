package chat_respond

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
	userMsgID, ok := jc.PayloadUUID("user_message_id")
	if !ok || userMsgID == uuid.Nil {
		jc.Fail("validate", fmt.Errorf("missing user_message_id"))
		return nil
	}
	asstMsgID, ok := jc.PayloadUUID("assistant_message_id")
	if !ok || asstMsgID == uuid.Nil {
		jc.Fail("validate", fmt.Errorf("missing assistant_message_id"))
		return nil
	}
	turnID, ok := jc.PayloadUUID("turn_id")
	if !ok || turnID == uuid.Nil {
		jc.Fail("validate", fmt.Errorf("missing turn_id"))
		return nil
	}

	jc.Progress("respond", 5, "Generating response")
	out, err := chatmod.New(chatmod.UsecasesDeps{
		DB:           p.db,
		Log:          p.log,
		AI:           p.ai,
		Vec:          p.vec,
		Threads:      p.threads,
		Messages:     p.messages,
		State:        p.state,
		Summaries:    p.summaries,
		Docs:         p.docs,
		Turns:        p.turns,
		Path:         p.path,
		PathNodes:    p.pathNodes,
		NodeDocs:     p.nodeDocs,
		Concepts:     p.concepts,
		ConceptEdges: p.edges,
		ConceptState: p.mastery,
		ConceptModel: p.models,
		MisconRepo:   p.miscon,
		Sessions:     p.sessions,
		JobRuns:      p.jobRuns,
		Jobs:         p.jobs,
		Notify:       p.notify,
	}).Respond(jc.Ctx, chatmod.RespondInput{
		UserID:             jc.Job.OwnerUserID,
		ThreadID:           threadID,
		UserMessageID:      userMsgID,
		AssistantMessageID: asstMsgID,
		TurnID:             turnID,
		JobID:              jc.Job.ID,
		Attempt:            jc.Job.Attempts,
	})
	if err != nil {
		jc.Fail("respond", err)
		return nil
	}

	jc.Succeed("done", map[string]any{
		"thread_id":            threadID.String(),
		"user_message_id":      userMsgID.String(),
		"assistant_message_id": asstMsgID.String(),
		"assistant_text_chars": len(out.AssistantText),
	})
	return nil
}
