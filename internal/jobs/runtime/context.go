package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type Context struct {
	Ctx         context.Context
	DB          *gorm.DB
	Job         *types.JobRun
	Repo        repos.JobRunRepo
	Notify      services.JobNotifier
	LastMessage string // Convenience: pipeline can write human messages without deciding event type
	payload     map[string]any
}

func NewContext(ctx context.Context, db *gorm.DB, job *types.JobRun, repo repos.JobRunRepo, notify services.JobNotifier) *Context {
	c := &Context{
		Ctx:    ctx,
		DB:     db,
		Job:    job,
		Repo:   repo,
		Notify: notify,
	}
	_ = c.decodePayload()
	return c
}

func (c *Context) decodePayload() error {
	if c.Job == nil {
		return nil
	}
	if len(c.Job.Payload) == 0 {
		c.payload = map[string]any{}
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(c.Job.Payload, &m); err != nil {
		c.payload = map[string]any{}
		return err
	}
	c.payload = m
	return nil
}

func (c *Context) Payload() map[string]any {
	if c.payload == nil {
		c.payload = map[string]any{}
	}
	return c.payload
}

func (c *Context) PayloadUUID(key string) (uuid.UUID, bool) {
	v, ok := c.Payload()[key]
	if !ok || v == nil {
		return uuid.Nil, false
	}
	s := fmt.Sprint(v)
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}

func (c *Context) Update(updates map[string]any) error {
	if c.Job == nil || c.Job.ID == uuid.Nil {
		return nil
	}
	_, err := c.Repo.UpdateFieldsUnlessStatus(c.Ctx, nil, c.Job.ID, []string{"canceled"}, toIfaceMap(updates))
	return err
}

func (c *Context) Progress(stage string, pct int, msg string) {
	if c == nil {
		return
	}
	ctx := c.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	now := time.Now()

	if c.Repo != nil && c.Job != nil && c.Job.ID != uuid.Nil {
		ok, _ := c.Repo.UpdateFieldsUnlessStatus(ctx, nil, c.Job.ID, []string{"canceled"}, map[string]interface{}{
			"stage":        stage,
			"progress":     pct,
			"message":      msg,
			"heartbeat_at": now,
			"updated_at":   now,
		})
		if !ok {
			return
		}
	}

	if c.Job != nil {
		c.Job.Stage = stage
		c.Job.Progress = pct
		c.Job.Message = msg
		c.Job.HeartbeatAt = &now
		c.Job.UpdatedAt = now
		// status remains whatever it is in DB ("running" after claim)
	}

	if c.Notify != nil && c.Job != nil {
		c.Notify.JobProgress(c.Job.OwnerUserID, c.Job, stage, pct, msg)
	}
}

func (c *Context) Fail(stage string, err error) {
	if c == nil {
		return
	}
	ctx := c.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	now := time.Now()
	msg := ""
	if err != nil {
		msg = err.Error()
	}

	if c.Repo != nil && c.Job != nil && c.Job.ID != uuid.Nil {
		ok, _ := c.Repo.UpdateFieldsUnlessStatus(ctx, nil, c.Job.ID, []string{"canceled"}, map[string]interface{}{
			"status":        "failed",
			"stage":         stage,
			"message":       "",
			"error":         msg,
			"last_error_at": now,
			"locked_at":     nil,
			"updated_at":    now,
		})
		if !ok {
			return
		}
	}

	if c.Job != nil {
		c.Job.Status = "failed"
		c.Job.Stage = stage
		c.Job.Message = ""
		c.Job.Error = msg
		c.Job.LastErrorAt = &now
		c.Job.LockedAt = nil
		c.Job.UpdatedAt = now
	}

	if c.Notify != nil && c.Job != nil {
		c.Notify.JobFailed(c.Job.OwnerUserID, c.Job, stage, msg)
	}
}

func (c *Context) Succeed(finalStage string, result any) {
	if c == nil {
		return
	}
	ctx := c.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	now := time.Now()
	var res datatypes.JSON
	if result != nil {
		b, _ := json.Marshal(result)
		res = datatypes.JSON(b)
	}

	if c.Repo != nil && c.Job != nil && c.Job.ID != uuid.Nil {
		ok, _ := c.Repo.UpdateFieldsUnlessStatus(ctx, nil, c.Job.ID, []string{"canceled"}, map[string]interface{}{
			"status":       "succeeded",
			"stage":        finalStage,
			"progress":     100,
			"message":      "",
			"error":        "",
			"result":       res,
			"locked_at":    nil,
			"heartbeat_at": now,
			"updated_at":   now,
		})
		if !ok {
			return
		}
	}

	if c.Job != nil {
		c.Job.Status = "succeeded"
		c.Job.Stage = finalStage
		c.Job.Progress = 100
		c.Job.Message = ""
		c.Job.Error = ""
		c.Job.Result = res
		c.Job.LockedAt = nil
		c.Job.HeartbeatAt = &now
		c.Job.UpdatedAt = now
	}

	if c.Notify != nil && c.Job != nil {
		c.Notify.JobDone(c.Job.OwnerUserID, c.Job)
	}
}

func toIfaceMap(in map[string]any) map[string]interface{} {
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
