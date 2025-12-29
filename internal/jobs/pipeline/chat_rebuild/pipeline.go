package chat_rebuild

import (
	"fmt"

	"github.com/google/uuid"

	"github.com/yungbote/neurobridge-backend/internal/jobs/chat/steps"
	jobrt "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
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

	jc.Progress("rebuild", 5, "Rebuilding chat projections")
	if err := steps.RebuildThreadProjections(jc.Ctx, steps.RebuildDeps{
		DB:  p.db,
		Log: p.log,
		Vec: p.vec,
	}, steps.RebuildInput{
		UserID:   jc.Job.OwnerUserID,
		ThreadID: threadID,
	}); err != nil {
		jc.Fail("rebuild", err)
		return nil
	}

	// Enqueue maintenance to rebuild derived artifacts from SQL truth.
	if p.jobs != nil && p.jobRuns != nil {
		dbc := dbctx.Context{Ctx: jc.Ctx, Tx: p.db}
		has, _ := p.jobRuns.HasRunnableForEntity(dbc, jc.Job.OwnerUserID, "chat_thread", threadID, "chat_maintain")
		if !has {
			payload := map[string]any{"thread_id": threadID.String()}
			entityID := threadID
			_, _ = p.jobs.Enqueue(dbc, jc.Job.OwnerUserID, "chat_maintain", "chat_thread", &entityID, payload)
		}
	}

	jc.Succeed("done", map[string]any{
		"thread_id": threadID.String(),
	})
	return nil
}
