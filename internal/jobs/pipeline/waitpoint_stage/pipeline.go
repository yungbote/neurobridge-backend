package waitpoint_stage

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	jobrt "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

func (p *Pipeline) Run(jc *jobrt.Context) error {
	if jc == nil || jc.Job == nil {
		return nil
	}
	if p == nil || p.db == nil || p.threads == nil || p.messages == nil {
		jc.Fail("validate", fmt.Errorf("waitpoint_stage: pipeline not configured"))
		return nil
	}

	if env := parseWaitpointEnvelope(jc.Job.Result); env != nil {
		if strings.TrimSpace(env.State.LastUserMessageID) != "" || env.State.Attempts > 0 {
			jc.Succeed("done", map[string]any{
				"status":    "resumed",
				"waitpoint": env.Waitpoint,
				"state":     env.State,
			})
			return nil
		}
	}

	setID, _ := jc.PayloadUUID("material_set_id")
	sagaID, _ := jc.PayloadUUID("saga_id")
	pathID, _ := jc.PayloadUUID("path_id")
	threadID, _ := jc.PayloadUUID("thread_id")
	if threadID == uuid.Nil {
		jc.Succeed("done", map[string]any{"status": "skipped_no_thread"})
		return nil
	}

	stageName := strings.TrimSpace(stringFromAny(jc.Payload()["stage_name"]))
	if stageName == "" {
		stageName = "waitpoint"
	}

	stageCfg := stageConfig(jc.Payload())
	cfg := jobrt.StageWaitpointConfig(jc.Payload())
	sourceOutputs := mapFromAny(jc.Payload()["source_outputs"])

	waitpointRequired := true
	if v := jc.Payload()["waitpoint_required"]; v != nil {
		waitpointRequired = boolFromAny(v)
	}
	if sourceOutputs != nil {
		if v, ok := sourceOutputs["needs_confirmation"]; ok {
			waitpointRequired = boolFromAny(v)
		}
	}

	if !waitpointRequired {
		jc.Succeed("done", map[string]any{"status": "skipped"})
		return nil
	}

	prompt := strings.TrimSpace(firstString(sourceOutputs, "prompt", "prompt_md", "question", "message"))
	if prompt == "" {
		if v := jc.Payload()["waitpoint_prompt"]; v != nil {
			prompt = strings.TrimSpace(stringFromAny(v))
		}
	}
	if prompt == "" && cfg != nil {
		prompt = strings.TrimSpace(stringFromAny(cfg["prompt"]))
	}
	if prompt == "" {
		prompt = "Please confirm how you'd like to proceed."
	}

	messageKind := strings.TrimSpace(stringFromAny(stageCfg["message_kind"]))
	if messageKind == "" {
		messageKind = "waitpoint_prompt"
	}

	var workflow any
	if sourceOutputs != nil {
		workflow = sourceOutputs["workflow"]
	}

	created, err := p.appendWaitpointMessage(
		jc,
		jc.Job.OwnerUserID,
		threadID,
		jc.Job.ID,
		messageKind,
		stageName,
		setID,
		pathID,
		prompt,
		workflow,
	)
	if err != nil {
		jc.Fail("message", err)
		return nil
	}

	minSeq := int64(0)
	if created != nil {
		minSeq = created.Seq
	}

	spec := jobrt.WaitpointSpec{
		Version:  1,
		Kind:     "yaml_intent_v1",
		Step:     stageName,
		Blocking: true,
		ThreadID: threadID.String(),
		MinSeq:   minSeq,
	}
	if cfg != nil {
		spec = jobrt.ApplyWaitpointConfig(spec, cfg)
	}

	state := jobrt.WaitpointState{
		Version: 1,
		Phase:   "awaiting_choice",
	}

	data := map[string]any{
		"material_set_id": setID.String(),
		"saga_id":         sagaID.String(),
		"path_id":         pathID.String(),
		"thread_id":       threadID.String(),
	}
	if sourceOutputs != nil && len(sourceOutputs) > 0 {
		data["source_outputs"] = sourceOutputs
		if v := sourceOutputs["intake"]; v != nil {
			data["intake"] = v
		}
		if v := sourceOutputs["options"]; v != nil {
			data["options"] = v
		}
		if v := sourceOutputs["files"]; v != nil {
			data["files"] = v
		}
		if v := sourceOutputs["meta"]; v != nil {
			data["meta"] = v
		}
	}
	if cfg != nil {
		data["waitpoint_config"] = cfg
	}

	jc.WaitForUser(
		"waiting_user",
		3,
		"Waiting for your response…",
		spec,
		state,
		data,
	)
	return nil
}

func (p *Pipeline) appendWaitpointMessage(
	jc *jobrt.Context,
	owner uuid.UUID,
	threadID uuid.UUID,
	jobID uuid.UUID,
	kind string,
	stageName string,
	materialSetID uuid.UUID,
	pathID uuid.UUID,
	content string,
	workflow any,
) (*types.ChatMessage, error) {
	if p.db == nil || p.threads == nil || p.messages == nil {
		return nil, fmt.Errorf("missing chat deps")
	}
	if owner == uuid.Nil || threadID == uuid.Nil || jobID == uuid.Nil {
		return nil, fmt.Errorf("missing ids")
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, fmt.Errorf("empty content")
	}
	if strings.TrimSpace(kind) == "" {
		kind = "waitpoint_prompt"
	}

	var created *types.ChatMessage
	createdNew := false

	err := p.db.WithContext(jc.Ctx).Transaction(func(tx *gorm.DB) error {
		inner := dbctx.Context{Ctx: jc.Ctx, Tx: tx}
		th, err := p.threads.LockByID(inner, threadID)
		if err != nil {
			return err
		}
		if th == nil || th.ID == uuid.Nil || th.UserID != owner {
			return fmt.Errorf("thread not found")
		}

		// Idempotency: one prompt message per waitpoint stage job.
		var existing types.ChatMessage
		e := tx.WithContext(jc.Ctx).
			Model(&types.ChatMessage{}).
			Where("thread_id = ? AND user_id = ? AND metadata->>'kind' = ? AND metadata->>'job_id' = ?", threadID, owner, kind, jobID.String()).
			First(&existing).Error
		if e == nil && existing.ID != uuid.Nil {
			created = &existing
			return nil
		}
		if e != nil && e != gorm.ErrRecordNotFound {
			return e
		}

		now := time.Now().UTC()
		meta := map[string]any{
			"kind":            kind,
			"job_id":          jobID.String(),
			"stage":           stageName,
			"path_id":         pathID.String(),
			"material_set_id": materialSetID.String(),
		}
		if workflow != nil {
			meta["workflow_v1"] = workflow
		}
		metaJSON, _ := json.Marshal(meta)

		nextSeq := th.NextSeq + 1
		msg := &types.ChatMessage{
			ID:        uuid.New(),
			ThreadID:  threadID,
			UserID:    owner,
			Seq:       nextSeq,
			Role:      "assistant",
			Status:    "sent",
			Content:   content,
			Model:     "",
			Metadata:  datatypes.JSON(metaJSON),
			CreatedAt: now,
			UpdatedAt: now,
		}
		if _, err := p.messages.Create(inner, []*types.ChatMessage{msg}); err != nil {
			return err
		}
		if err := p.threads.UpdateFields(inner, threadID, map[string]interface{}{
			"next_seq":        nextSeq,
			"last_message_at": now,
			"updated_at":      now,
		}); err != nil {
			return err
		}

		created = msg
		createdNew = true
		return nil
	})
	if err != nil {
		return nil, err
	}

	if createdNew && created != nil && p.notify != nil {
		p.notify.MessageCreated(owner, threadID, created, nil)
	}
	return created, nil
}

func parseWaitpointEnvelope(raw datatypes.JSON) *jobrt.WaitpointEnvelope {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "" ||
		strings.TrimSpace(string(raw)) == "null" {
		return nil
	}
	var env jobrt.WaitpointEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil
	}
	return &env
}

func stageConfig(payload map[string]any) map[string]any {
	if payload == nil {
		return nil
	}
	raw, ok := payload["stage_config"]
	if !ok || raw == nil {
		return nil
	}
	if m, ok := raw.(map[string]any); ok {
		return m
	}
	return nil
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

func boolFromAny(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		s := strings.ToLower(strings.TrimSpace(x))
		return s == "true" || s == "1" || s == "yes" || s == "y"
	default:
		s := strings.ToLower(strings.TrimSpace(fmt.Sprint(v)))
		return s == "true" || s == "1" || s == "yes" || s == "y"
	}
}

func mapFromAny(v any) map[string]any {
	if v == nil {
		return nil
	}
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

func firstString(m map[string]any, keys ...string) string {
	if m == nil {
		return ""
	}
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s := strings.TrimSpace(stringFromAny(v)); s != "" {
				return s
			}
		}
	}
	return ""
}
