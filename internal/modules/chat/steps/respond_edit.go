package steps

import (
	"context"
	"encoding/json"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"gorm.io/datatypes"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

type editHandleResult struct {
	Handled bool
	Reply   string
	Meta    map[string]any
	Err     error
}

func maybeHandleEditRequest(
	ctx context.Context,
	deps RespondDeps,
	in RespondInput,
	thread *types.ChatThread,
	plan ContextPlanOutput,
	userText string,
) editHandleResult {
	out := editHandleResult{}
	if !strings.EqualFold(strings.TrimSpace(plan.Mode), "edit") {
		return out
	}
	if isWaitpointCommand(userText) {
		out.Handled = true
		out.Reply = "I don’t have a pending edit to confirm or deny right now."
		out.Meta = map[string]any{"kind": "node_doc_edit_noop"}
		return out
	}
	out.Handled = true
	if plan.EditTarget == nil || strings.TrimSpace(plan.EditTarget.BlockID) == "" || strings.TrimSpace(plan.EditTarget.PathNodeID) == "" {
		out.Reply = "I can edit the current block, but I don’t have a fresh on-screen block to target. Scroll the block you want and try again."
		out.Meta = map[string]any{"kind": "node_doc_edit_missing_target"}
		return out
	}
	if deps.Jobs == nil {
		out.Reply = "Editing isn’t available right now."
		out.Meta = map[string]any{"kind": "node_doc_edit_unavailable"}
		return out
	}
	nodeID, err := uuid.Parse(strings.TrimSpace(plan.EditTarget.PathNodeID))
	if err != nil || nodeID == uuid.Nil {
		out.Reply = "I couldn’t identify the current block to edit. Please try again."
		out.Meta = map[string]any{"kind": "node_doc_edit_missing_target"}
		return out
	}
	payload := map[string]any{
		"thread_id":       thread.ID.String(),
		"path_node_id":    nodeID.String(),
		"block_id":        strings.TrimSpace(plan.EditTarget.BlockID),
		"block_index":     plan.EditTarget.BlockIndex,
		"action":          "rewrite",
		"citation_policy": "reuse_only",
		"instruction":     strings.TrimSpace(userText),
	}
	entityID := nodeID
	job, err := deps.Jobs.Enqueue(dbctx.Context{Ctx: ctx, Tx: deps.DB}, in.UserID, "node_doc_edit", "path_node", &entityID, payload)
	if err != nil {
		out.Reply = "I couldn’t start the edit draft. Please try again."
		out.Meta = map[string]any{"kind": "node_doc_edit_enqueue_failed", "error": err.Error()}
		return out
	}
	out.Reply = "Drafting a revision for the current block. I’ll show a diff for confirmation."
	out.Meta = map[string]any{
		"kind":         "node_doc_edit_pending",
		"job_id":       job.ID.String(),
		"path_node_id": nodeID.String(),
		"block_id":     strings.TrimSpace(plan.EditTarget.BlockID),
	}
	return out
}

func isWaitpointCommand(text string) bool {
	normalized := strings.ToLower(strings.TrimSpace(text))
	if normalized == "" {
		return false
	}
	normalized = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsLetter(r) || unicode.IsNumber(r):
			return r
		case unicode.IsSpace(r):
			return ' '
		default:
			return -1
		}
	}, normalized)
	normalized = strings.Join(strings.Fields(normalized), " ")
	if normalized == "" {
		return false
	}
	words := strings.Fields(normalized)
	if len(words) > 3 {
		return false
	}
	switch normalized {
	case "confirm", "approve", "apply", "accept", "yes", "yep", "ok", "okay":
		return true
	case "deny", "decline", "reject", "no", "cancel", "discard", "keep", "stop":
		return true
	case "refine", "change", "revise", "edit", "rewrite":
		return true
	case "no thanks", "not now", "dont change", "dont edit", "keep it", "leave it":
		return true
	default:
		return false
	}
}

func finalizeImmediateReply(
	ctx context.Context,
	deps RespondDeps,
	in RespondInput,
	reply string,
	meta map[string]any,
) error {
	if deps.Messages == nil || deps.Turns == nil {
		return nil
	}
	now := time.Now().UTC()
	metaJSON, _ := json.Marshal(meta)
	_ = deps.Messages.UpdateFields(dbctx.Context{Ctx: ctx, Tx: deps.DB}, in.AssistantMessageID, map[string]any{
		"content":    strings.TrimSpace(reply),
		"status":     MessageStatusDone,
		"metadata":   datatypes.JSON(metaJSON),
		"updated_at": now,
	})
	if deps.Notify != nil {
		var asst types.ChatMessage
		_ = deps.DB.WithContext(ctx).
			Model(&types.ChatMessage{}).
			Where("id = ? AND thread_id = ? AND user_id = ?", in.AssistantMessageID, in.ThreadID, in.UserID).
			First(&asst).Error
		if asst.ID != uuid.Nil {
			deps.Notify.MessageDone(in.UserID, in.ThreadID, &asst, map[string]any{
				"turn_id": in.TurnID.String(),
				"attempt": in.Attempt,
			})
		}
	}
	doneAt := time.Now().UTC()
	_ = deps.Turns.UpdateFields(dbctx.Context{Ctx: ctx, Tx: deps.DB}, in.UserID, in.TurnID, map[string]any{
		"status":       "done",
		"completed_at": &doneAt,
	})
	return nil
}
