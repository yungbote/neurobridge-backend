package web_resources_seed

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"

	jobrt "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
	learningmod "github.com/yungbote/neurobridge-backend/internal/modules/learning"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
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

	threadID, _ := jc.PayloadUUID("thread_id")
	pathID, _ := jc.PayloadUUID("path_id")

	prompt := ""
	if v, ok := jc.Payload()["prompt"]; ok && v != nil {
		prompt = fmt.Sprint(v)
	}

	jc.Progress("seed", 2, "Seeding learning materials")
	out, err := learningmod.New(learningmod.UsecasesDeps{
		DB:        p.db,
		Log:       p.log,
		Files:     p.files,
		Path:      p.path,
		Bucket:    p.bucket,
		Threads:   p.threads,
		Messages:  p.messages,
		Notify:    p.notify,
		AI:        p.ai,
		Saga:      p.saga,
		Bootstrap: p.bootstrap,
	}).WebResourcesSeed(jc.Ctx, learningmod.WebResourcesSeedInput{
		OwnerUserID:   jc.Job.OwnerUserID,
		MaterialSetID: setID,
		SagaID:        sagaID,
		PathID:        pathID,
		Prompt:        prompt,
		ThreadID:      threadID,
		JobID:         jc.Job.ID,
		WaitForUser:   true,
	})
	if err != nil {
		jc.Fail("seed", err)
		return nil
	}

	if strings.EqualFold(strings.TrimSpace(out.Status), "waiting_user") {
		pauseForUser(jc, setID, sagaID, out)
		return nil
	}

	jc.Succeed("done", map[string]any{
		"material_set_id":   setID.String(),
		"saga_id":           sagaID.String(),
		"path_id":           out.PathID.String(),
		"skipped":           out.Skipped,
		"files_created":     out.FilesCreated,
		"resources_planned": out.ResourcesPlanned,
		"resources_fetched": out.ResourcesFetched,
	})
	return nil
}

func pauseForUser(jc *jobrt.Context, setID, sagaID uuid.UUID, out learningmod.WebResourcesSeedOutput) {
	if jc == nil || jc.Job == nil || jc.Repo == nil {
		return
	}
	now := time.Now().UTC()
	resObj := map[string]any{
		"material_set_id": setID.String(),
		"saga_id":         sagaID.String(),
		"path_id":         out.PathID.String(),
		"status":          "waiting_user",
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

	jc.Progress("waiting_user", 3, "Waiting for your response…")
}
