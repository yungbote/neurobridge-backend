package runtime

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"

	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

/*
WaitpointAction is intentionally small.
It can be used for UI hints or deterministic matching,
but in the config-driven system it is mostly metadata.
*/
type WaitpointAction struct {
	ID      string `json:"id"`
	Label   string `json:"label,omitempty"`
	Token   string `json:"token,omitempty"`
	Variant string `json:"variant,omitempty"`
}

// ApplyWaitpointConfig overrides spec fields from a config map.
// Supported keys: kind, step, actions (list of {id,label,token,variant}).
func ApplyWaitpointConfig(spec WaitpointSpec, cfg map[string]any) WaitpointSpec {
	if cfg == nil {
		return spec
	}
	if v := stringFromAny(cfg["kind"]); strings.TrimSpace(v) != "" {
		spec.Kind = strings.TrimSpace(v)
	}
	if v := stringFromAny(cfg["step"]); strings.TrimSpace(v) != "" {
		spec.Step = strings.TrimSpace(v)
	}
	if actions := cfg["actions"]; actions != nil {
		spec.Actions = parseWaitpointActions(actions)
	}
	return spec
}

func parseWaitpointActions(v any) []WaitpointAction {
	raw, ok := v.([]any)
	if !ok {
		if typed, ok := v.([]WaitpointAction); ok {
			return typed
		}
		return nil
	}
	out := make([]WaitpointAction, 0, len(raw))
	for _, it := range raw {
		m, ok := it.(map[string]any)
		if !ok || m == nil {
			continue
		}
		id := strings.TrimSpace(stringFromAny(m["id"]))
		if id == "" {
			continue
		}
		out = append(out, WaitpointAction{
			ID:      id,
			Label:   strings.TrimSpace(stringFromAny(m["label"])),
			Token:   strings.TrimSpace(stringFromAny(m["token"])),
			Variant: strings.TrimSpace(stringFromAny(m["variant"])),
		})
	}
	return out
}

func stringFromAny(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []byte:
		return string(t)
	default:
		return ""
	}
}

/*
WaitpointSpec describes what the job is waiting for.
Kind is the registry key for the interpreter config (e.g., "path_intake.choose_structure")
Blocking=true means the job is paused.
*/
type WaitpointSpec struct {
	Version  int               `json:"version"`
	Kind     string            `json:"kind"`
	Step     string            `json:"step,omitempty"`
	Blocking bool              `json:"blocking"`
	ThreadID string            `json:"thread_id,omitempty"`
	MinSeq   int64             `json:"min_seq,omitempty"`
	Actions  []WaitpointAction `json:"actions,omitempty"`
}

/*
WaitpointState is interpreter-owned durable state.
This is mutated by the waitpoint_interpret job to avoid loops.
*/
type WaitpointState struct {
	Version            int     `json:"version"`
	Phase              string  `json:"phase,omitempty"`
	LastUserMessageID  string  `json:"last_user_message_id,omitempty"`
	LastUserSeqHandled int64   `json:"last_user_seq_handled,omitempty"`
	PendingGuess       string  `json:"pending_guess,omitempty"`
	Attempts           int     `json:"attempts,omitempty"`
	LastCase           string  `json:"last_case,omitempty"`
	LastConfidence     float64 `json:"last_confidence,omitempty"`
}

/*
WaitpointEnvelope is stored in job_run.result when status=waiting_user.
*/
type WaitpointEnvelope struct {
	Waitpoint WaitpointSpec  `json:"waitpoint"`
	State     WaitpointState `json:"state,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
}

/*
WaitForUser is a durable pause primitive.
It:
  - sets job_run.status = waiting_user,
  - clears locked_at,
  - stores a machine-readable waitpoint envelope in job_run.result,
  - emits a progress update.
*/
func (c *Context) WaitForUser(
	stage string,
	pct int,
	msg string,
	spec WaitpointSpec,
	state WaitpointState,
	data map[string]any,
) {
	if c == nil || c.Job == nil || c.Job.ID == uuid.Nil {
		return
	}
	ctx := c.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	now := time.Now().UTC()
	if strings.TrimSpace(stage) == "" {
		stage = "waiting_user"
	}
	if strings.TrimSpace(msg) == "" {
		msg = "Waiting for your response..."
	}
	if pct < 0 {
		pct = 0
	}
	if pct > 99 {
		pct = 99
	}
	if spec.Version <= 0 {
		spec.Version = 1
	}
	if strings.TrimSpace(spec.Kind) == "" {
		spec.Kind = "unknown"
	}

	spec.Blocking = true

	if state.Version <= 0 {
		state.Version = 1
	}

	env := WaitpointEnvelope{
		Waitpoint: spec,
		State:     state,
		Data:      data,
	}
	b, _ := json.Marshal(env)
	res := datatypes.JSON(b)

	if c.Repo != nil {
		_, _ = c.Repo.UpdateFieldsUnlessStatus(
			dbctx.Context{Ctx: ctx},
			c.Job.ID,
			[]string{"canceled"},
			map[string]interface{}{
				"status":       "waiting_user",
				"stage":        stage,
				"progress":     pct,
				"message":      msg,
				"error":        "",
				"result":       res,
				"locked_at":    nil,
				"heartbeat_at": now,
				"updated_at":   now,
			},
		)
	}

	c.Job.Status = "waiting_user"
	c.Job.Stage = stage
	c.Job.Progress = pct
	c.Job.Message = msg
	c.Job.Error = ""
	c.Job.Result = res
	c.Job.LockedAt = nil
	c.Job.HeartbeatAt = &now
	c.Job.UpdatedAt = now

	if c.Notify != nil {
		c.Notify.JobProgress(c.Job.OwnerUserID, c.Job, stage, pct, msg)
	}
}
