package chat_purge

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

	jc.Progress("purge", 5, "Purging chat artifacts")
	if err := steps.PurgeThreadArtifacts(jc.Ctx, steps.RebuildDeps{
		DB:  p.db,
		Log: p.log,
		Vec: p.vec,
	}, steps.RebuildInput{
		UserID:   jc.Job.OwnerUserID,
		ThreadID: threadID,
	}); err != nil {
		jc.Fail("purge", err)
		return nil
	}

	jc.Succeed("done", map[string]any{
		"thread_id": threadID.String(),
	})
	return nil
}
