package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/ctxutil"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

/*
The execution contract between job system and all business code.
runtime.Context is a capability-scoped execution handle for a single job run.
It wraps:
	- The database transaction boundary,
	- The mutable job_run row,
	- The notification side-effects,
	- And the only sanctioned ways to report progress or terminate execution
Struct:
	- Ctx: request-scoped context.Context (timeouts, cancellation)
	- DB: DB handle (used by pipelines/usecases)
	- Job: The JobRUn row in memory
	- Notify: Side-channel notifier (SSE)
	- payload: decoded job input
*Pipelines never touch job_run directly. They must go through this object.*
*/

type Context struct {
	Ctx         context.Context
	DB          *gorm.DB
	Job         *types.JobRun
	Repo        repos.JobRunRepo
	Notify      services.JobNotifier
	LastMessage string // Convenience: pipeline can write human messages without deciding event type
	payload     map[string]any
}

/*
NewContext constructs a runtime.Context for a claimed job execution.
It eagerly decodes the job payload JSON so handlers can access inputs via Payload()/PayloadUUID().
Any payload decode failure is treated as non-fata here; handlers typically validate required fields.
*/
func NewContext(ctx context.Context, db *gorm.DB, job *types.JobRun, repo repos.JobRunRepo, notify services.JobNotifier) *Context {
	c := &Context{
		Ctx:    ctx,
		DB:     db,
		Job:    job,
		Repo:   repo,
		Notify: notify,
	}
	_ = c.decodePayload()
	c.applyTraceData()
	return c
}

/*
decodePayload parse Job.Payload JSON into map for access.
Invariants / behavior:
	- If Job is nil: no-op
	- If payload is empty: sets payload to empty map
	- On unmarshal error: sets payload to empty map and returns the error,
	  allowing callers to decide whether malformed payload should fail the job.
*/
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

func (c *Context) applyTraceData() {
	if c == nil || c.Ctx == nil {
		return
	}
	payload := c.Payload()
	traceID := strings.TrimSpace(fmt.Sprint(payload["trace_id"]))
	reqID := strings.TrimSpace(fmt.Sprint(payload["request_id"]))
	if traceID == "" && reqID == "" {
		return
	}
	c.Ctx = ctxutil.WithTraceData(c.Ctx, &ctxutil.TraceData{
		TraceID:   traceID,
		RequestID: reqID,
	})
}

/*
Payload returns the decoded payload map for this job execution.
Guarantees:
	- Never returns nil (returns an empty map if payload is unset/unparseable)
	- The map represents the JSON object stored on Job.Payload, not Job.Result
*/
func (c *Context) Payload() map[string]any {
	if c.payload == nil {
		c.payload = map[string]any{}
	}
	return c.payload
}

/*
PayloadUUID reads a payload field by key and attempts to parse it as a UUID.
Returns:
	- (uuid, true) if key exists and parses cleanly as a non-empty UUID string
	- (uuid.Nil, false) if missing, nil, or not parseable
This keeps UUID validation logic out of pipelines and makes payload parsing uniform.
*/
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

/*
Update applies arbitrary field updates to the underlying job_run row in storage,
guarded by "UnlessStatus(canceled)"
Intended use:
	- low-level state writes (e.g., orchestrator state snapshots into result)
	- rare custom transitions not covered by Progress/Fail/Succeed
Not intended as a general replacement for Progress/Fail/Succeed. Prefer those for
lifecycle transitions so invariants remain centralized.
*/
func (c *Context) Update(updates map[string]any) error {
	if c.Job == nil || c.Job.ID == uuid.Nil {
		return nil
	}
	_, err := c.Repo.UpdateFieldsUnlessStatus(dbctx.Context{Ctx: c.Ctx}, c.Job.ID, []string{"canceled"}, toIfaceMap(updates))
	return err
}

/*
Progress publishes a non-terminal status update for this job run.
What it does:
	- Persists stage/progress/message + heartbeat timestamps into job_run,
	  guarded so canceled jobs are not overwritten.
	- Updates the in-memory c.Job fields to match.
	- Emits a notifier event so clients can update UI promptly.
*/
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
		ok, _ := c.Repo.UpdateFieldsUnlessStatus(dbctx.Context{Ctx: ctx}, c.Job.ID, []string{"canceled"}, map[string]interface{}{
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

/*
Fail marks this job run as terminally failed and records an error message.
What it does:
	- Sets status=failed, stage=<stage>, error=<err>, last_error_at=now
	- Clears locked_at so other workers won't treat it as in-progress
	- Updates in-memory job object
	- Emits a 'failed' notification
Guarding:
	- Uses UpdateFieldsUnlessStatus(..., ["canceled"]) so a canceled job is not overwritten
	- If update is rejected, exists witout emitting notifications
*/
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
		ok, _ := c.Repo.UpdateFieldsUnlessStatus(dbctx.Context{Ctx: ctx}, c.Job.ID, []string{"canceled"}, map[string]interface{}{
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

/*
Succeed marks this job run as terminally succeeded and persists a result payload.
What is does:
	- Sets status=succeeded, stage=<finalStage>, progress=100
	- Clears error/message, clears locked_at, updates heartbeat
	- Serializes 'result' as JSON and stored it in job_run.result
	- Updates in-memory job object
	- Emits a 'done' notification
Guarding:
	- Uses UpdateFieldsUnlessStatus(..., ["canceled"]) so a canceled job is not overwritten
	- If update is rejected, exists without emitting notifications
*/
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
		ok, _ := c.Repo.UpdateFieldsUnlessStatus(dbctx.Context{Ctx: ctx}, c.Job.ID, []string{"canceled"}, map[string]interface{}{
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

/*
toIfaceMap converts a map[string]any into map[string]interface{}.
This exists because some repository APIs take map[string]interface{} for DB updates,
but callers usually build map[string]any. It keeps the conversion centralized and
avoids repeating boilerplate at call sites.
*/
func toIfaceMap(in map[string]any) map[string]interface{} {
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}








