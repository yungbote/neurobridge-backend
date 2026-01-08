package path_intake

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"

	"github.com/yungbote/neurobridge-backend/internal/jobs/learning/steps"
	jobrt "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
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

	pathID, _ := jc.PayloadUUID("path_id")
	threadID, _ := jc.PayloadUUID("thread_id")

	jc.Progress("intake", 2, "Reviewing your materials")
	out, err := steps.PathIntake(jc.Ctx, steps.PathIntakeDeps{
		DB:        p.db,
		Log:       p.log,
		Files:     p.files,
		Chunks:    p.chunks,
		Summaries: p.summaries,
		Path:      p.path,
		Prefs:     p.prefs,
		Threads:   p.threads,
		Messages:  p.messages,
		AI:        p.ai,
		Notify:    p.notify,
		Bootstrap: p.bootstrap,
	}, steps.PathIntakeInput{
		OwnerUserID:   jc.Job.OwnerUserID,
		MaterialSetID: setID,
		SagaID:        sagaID,
		PathID:        pathID,
		ThreadID:      threadID,
		JobID:         jc.Job.ID,
		WaitForUser:   true,
	})
	if err != nil {
		jc.Fail("intake", err)
		return nil
	}

	if strings.EqualFold(strings.TrimSpace(out.Status), "waiting_user") {
		pauseForUser(jc, setID, sagaID, out)
		return nil
	}

	jc.Succeed("done", map[string]any{
		"material_set_id": setID.String(),
		"saga_id":         sagaID.String(),
		"path_id":         out.PathID.String(),
		"thread_id":       out.ThreadID.String(),
		"intake":          out.Intake,
	})
	return nil
}

func pauseForUser(jc *jobrt.Context, setID, sagaID uuid.UUID, out steps.PathIntakeOutput) {
	if jc == nil || jc.Job == nil || jc.Repo == nil {
		return
	}
	now := time.Now().UTC()
	resObj := map[string]any{
		"material_set_id": setID.String(),
		"saga_id":         sagaID.String(),
		"path_id":         out.PathID.String(),
		"thread_id":       out.ThreadID.String(),
		"status":          "waiting_user",
		"intake":          out.Intake,
		"meta":            out.Meta,
	}
	b, _ := json.Marshal(resObj)

	_, _ = jc.Repo.UpdateFieldsUnlessStatus(dbctx.Context{Ctx: jc.Ctx, Tx: jc.DB}, jc.Job.ID, []string{"canceled"}, map[string]interface{}{
		"status":       "waiting_user",
		"stage":        "waiting_user",
		"progress":     3,
		"message":      "Waiting for your response…",
		"error":        "",
		"result":       datatypes.JSON(b),
		"locked_at":    nil,
		"heartbeat_at": now,
		"updated_at":   now,
	})

	jc.Job.Status = "waiting_user"
	jc.Job.Stage = "waiting_user"
	jc.Job.Progress = 3
	jc.Job.Message = "Waiting for your response…"
	jc.Job.Error = ""
	jc.Job.Result = datatypes.JSON(b)
	jc.Job.LockedAt = nil
	jc.Job.HeartbeatAt = &now
	jc.Job.UpdatedAt = now

	// Emit a progress update so clients receive the status change.
	jc.Progress("waiting_user", 3, "Waiting for your response…")
}
