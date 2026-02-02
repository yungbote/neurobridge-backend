package node_doc_edit

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
	learningmod "github.com/yungbote/neurobridge-backend/internal/modules/learning"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

func (p *Pipeline) Run(jc *jobrt.Context) error {
	if jc == nil || jc.Job == nil {
		return nil
	}
	if p.db == nil || p.log == nil || p.threads == nil || p.messages == nil {
		jc.Fail("validate", fmt.Errorf("node_doc_edit: missing deps"))
		return nil
	}

	threadID, ok := jc.PayloadUUID("thread_id")
	if !ok || threadID == uuid.Nil {
		jc.Fail("validate", fmt.Errorf("missing thread_id"))
		return nil
	}
	nodeID, ok := jc.PayloadUUID("path_node_id")
	if !ok || nodeID == uuid.Nil {
		jc.Fail("validate", fmt.Errorf("missing path_node_id"))
		return nil
	}

	payload := jc.Payload()
	blockID := strings.TrimSpace(fmt.Sprint(payload["block_id"]))
	blockIndex := parseInt(payload["block_index"], -1)
	action := strings.TrimSpace(fmt.Sprint(payload["action"]))
	instruction := strings.TrimSpace(fmt.Sprint(payload["instruction"]))
	citationPolicy := strings.TrimSpace(fmt.Sprint(payload["citation_policy"]))

	var sel learningmod.NodeDocPatchSelection
	if raw, ok := payload["selection"].(map[string]any); ok {
		sel.Text = strings.TrimSpace(fmt.Sprint(raw["text"]))
		sel.Start = parseInt(raw["start"], 0)
		sel.End = parseInt(raw["end"], 0)
	}

	jc.Progress("propose", 4, "Drafting revision")
	preview, err := learningmod.New(learningmod.UsecasesDeps{
		DB:        p.db,
		Log:       p.log,
		Path:      p.path,
		PathNodes: p.nodes,
		NodeDocs:  p.docs,
		Figures:   p.figures,
		Videos:    p.videos,
		Files:     p.files,
		Chunks:    p.chunks,
		ULI:       p.uli,
		Assets:    p.assets,
		AI:        p.ai,
		Vec:       p.vec,
		Bucket:    p.bucket,
	}).NodeDocPatchPreview(jc.Ctx, learningmod.NodeDocPatchInput{
		OwnerUserID:    jc.Job.OwnerUserID,
		PathNodeID:     nodeID,
		BlockID:        blockID,
		BlockIndex:     blockIndex,
		Action:         action,
		Instruction:    instruction,
		CitationPolicy: citationPolicy,
		Selection:      sel,
		JobID:          jc.Job.ID,
	})
	if err != nil {
		jc.Fail("propose", err)
		return nil
	}

	proposal := map[string]any{
		"path_node_id":    nodeID.String(),
		"path_id":         preview.PathID.String(),
		"doc_id":          preview.DocID.String(),
		"block_id":        preview.BlockID,
		"block_index":     blockIndex,
		"block_type":      preview.BlockType,
		"action":          preview.Action,
		"citation_policy": preview.CitationPolicy,
		"instruction":     instruction,
		"selection": map[string]any{
			"text":  strings.TrimSpace(sel.Text),
			"start": sel.Start,
			"end":   sel.End,
		},
		"before_block":      json.RawMessage(preview.BeforeBlockJSON),
		"after_block":       json.RawMessage(preview.AfterBlockJSON),
		"before_block_text": preview.BeforeBlockText,
		"after_block_text":  preview.AfterBlockText,
		"model":             preview.Model,
		"prompt_version":    preview.PromptVersion,
		"created_at":        time.Now().UTC().Format(time.RFC3339),
	}

	prompt := strings.TrimSpace(strings.Join([]string{
		"I drafted a revision for the current block.",
		"Review the changes and confirm to apply, or reply with what to change.",
	}, " "))

	cfg := map[string]any{
		"kind":           "yaml_intent_v1",
		"prompt":         prompt,
		"clarify_prompt": "Please confirm (apply), deny, or describe how to refine it.",
		"intents": []any{
			map[string]any{
				"id":              "confirm",
				"label":           "Apply",
				"description":     "apply the proposed edit",
				"action":          "confirm_resume",
				"selection":       map[string]any{"commit_type": "confirm"},
				"confirm_message": "Applied.",
			},
			map[string]any{
				"id":              "refine",
				"label":           "Refine",
				"description":     "revise the edit based on new guidance",
				"action":          "confirm_resume",
				"selection":       map[string]any{"commit_type": "change"},
				"confirm_message": "Okay — drafting a revised edit.",
			},
			map[string]any{
				"id":              "deny",
				"label":           "Discard",
				"description":     "discard the proposed edit",
				"action":          "confirm_resume",
				"selection":       map[string]any{"commit_type": "deny"},
				"confirm_message": "Okay, I won’t change it.",
			},
		},
	}

	created, err := p.appendEditMessage(jc, jc.Job.OwnerUserID, threadID, preview.PathID, nodeID, prompt, proposal)
	if err != nil {
		jc.Fail("message", err)
		return nil
	}
	minSeq := int64(0)
	if created != nil {
		minSeq = created.Seq
	}

	if err := p.setThreadPendingWaitpoint(jc, threadID, jc.Job.ID, proposal); err != nil {
		p.log.Warn("node_doc_edit: set pending waitpoint failed", "error", err)
	}

	spec := jobrt.WaitpointSpec{
		Version:  1,
		Kind:     "yaml_intent_v1",
		Step:     "node_doc_edit",
		Blocking: true,
		ThreadID: threadID.String(),
		MinSeq:   minSeq,
	}
	state := jobrt.WaitpointState{Version: 1, Phase: "awaiting_choice"}
	data := map[string]any{
		"thread_id":        threadID.String(),
		"path_id":          preview.PathID.String(),
		"path_node_id":     nodeID.String(),
		"proposal":         proposal,
		"waitpoint_config": cfg,
	}

	jc.WaitForUser("waiting_user", 6, "Waiting for your response…", spec, state, data)
	return nil
}

func (p *Pipeline) appendEditMessage(jc *jobrt.Context, owner uuid.UUID, threadID uuid.UUID, pathID uuid.UUID, nodeID uuid.UUID, content string, proposal map[string]any) (*types.ChatMessage, error) {
	if p.db == nil || p.threads == nil || p.messages == nil {
		return nil, fmt.Errorf("missing chat deps")
	}
	if owner == uuid.Nil || threadID == uuid.Nil {
		return nil, fmt.Errorf("missing ids")
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, fmt.Errorf("empty content")
	}

	var created *types.ChatMessage
	err := p.db.WithContext(jc.Ctx).Transaction(func(tx *gorm.DB) error {
		inner := dbctx.Context{Ctx: jc.Ctx, Tx: tx}
		th, err := p.threads.LockByID(inner, threadID)
		if err != nil {
			return err
		}
		if th == nil || th.ID == uuid.Nil || th.UserID != owner {
			return fmt.Errorf("thread not found")
		}

		// Idempotency: one edit prompt per job.
		var existing types.ChatMessage
		e := tx.WithContext(jc.Ctx).
			Model(&types.ChatMessage{}).
			Where("thread_id = ? AND user_id = ? AND metadata->>'kind' = ? AND metadata->>'job_id' = ?", threadID, owner, "node_doc_edit", jc.Job.ID.String()).
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
			"kind":         "node_doc_edit",
			"job_id":       jc.Job.ID.String(),
			"path_id":      pathID.String(),
			"path_node_id": nodeID.String(),
			"proposal":     proposal,
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
		return nil
	})
	if err != nil {
		return nil, err
	}
	if p.notify != nil && created != nil {
		p.notify.MessageCreated(owner, threadID, created, map[string]any{
			"job_id": jc.Job.ID.String(),
		})
	}
	return created, nil
}

func (p *Pipeline) setThreadPendingWaitpoint(jc *jobrt.Context, threadID uuid.UUID, jobID uuid.UUID, proposal map[string]any) error {
	if p.threads == nil {
		return nil
	}
	if threadID == uuid.Nil || jobID == uuid.Nil {
		return nil
	}
	var meta map[string]any
	if err := p.db.WithContext(jc.Ctx).Model(&types.ChatThread{}).Select("metadata").Where("id = ?", threadID).Scan(&meta).Error; err != nil {
		return err
	}
	if meta == nil {
		meta = map[string]any{}
	}
	meta["pending_waitpoint_job_id"] = jobID.String()
	meta["pending_waitpoint_kind"] = "node_doc_edit"
	if proposal != nil {
		meta["pending_waitpoint_proposal"] = proposal
	}
	metaJSON, _ := json.Marshal(meta)
	return p.threads.UpdateFields(dbctx.Context{Ctx: jc.Ctx, Tx: jc.DB}, threadID, map[string]interface{}{"metadata": datatypes.JSON(metaJSON)})
}

func parseInt(v any, def int) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case float32:
		return int(t)
	default:
		return def
	}
}
