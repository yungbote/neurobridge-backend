package services

import (
	"encoding/json"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

type workflowV1Meta struct {
	Version  int                    `json:"version"`
	Kind     string                 `json:"kind"`
	Step     string                 `json:"step"`
	Blocking bool                   `json:"blocking"`
	Actions  []workflowV1ActionMeta `json:"actions"`
}

type workflowV1ActionMeta struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Token   string `json:"token"`
	Variant string `json:"variant,omitempty"`
}

func activeBlockingWorkflowForPausedIntake(dbc dbctx.Context, threadID uuid.UUID, userID uuid.UUID) (*workflowV1Meta, error) {
	if dbc.Ctx == nil || dbc.Tx == nil || threadID == uuid.Nil || userID == uuid.Nil {
		return nil, nil
	}

	var msg types.ChatMessage
	err := dbc.Tx.WithContext(dbc.Ctx).
		Table("chat_message AS m").
		Select("m.*").
		Joins("JOIN job_run AS j ON j.id::text = m.metadata->>'job_id'").
		Where("m.thread_id = ? AND m.user_id = ? AND m.deleted_at IS NULL", threadID, userID).
		Where("m.role = ?", "assistant").
		Where("m.metadata->>'kind' = ?", "path_intake_questions").
		Where("j.owner_user_id = ? AND j.job_type = ? AND j.status = ?", userID, "path_intake", "waiting_user").
		Order("m.seq DESC").
		Limit(1).
		First(&msg).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	meta := jsonMapFromRaw(msg.Metadata)
	wf := parseWorkflowV1Meta(meta)
	if wf == nil || !wf.Blocking {
		return nil, nil
	}
	return wf, nil
}

func parseWorkflowV1Meta(messageMeta map[string]any) *workflowV1Meta {
	if messageMeta == nil {
		return nil
	}
	raw, ok := messageMeta["workflow_v1"]
	if !ok || raw == nil {
		return nil
	}
	b, err := json.Marshal(raw)
	if err != nil || len(b) == 0 {
		return nil
	}
	var wf workflowV1Meta
	if err := json.Unmarshal(b, &wf); err != nil {
		return nil
	}
	if wf.Version != 1 || strings.TrimSpace(wf.Kind) == "" || len(wf.Actions) == 0 {
		return nil
	}
	return &wf
}

func matchWorkflowV1Action(wf *workflowV1Meta, userContent string) (workflowV1ActionMeta, bool) {
	if wf == nil || len(wf.Actions) == 0 {
		return workflowV1ActionMeta{}, false
	}

	// 1) Exact (but flexible) token match.
	for _, a := range wf.Actions {
		if strings.TrimSpace(a.Token) == "" {
			continue
		}
		if workflowTokenMatches(userContent, a.Token) {
			return a, true
		}
	}

	// 2) Workflow-specific fallbacks (still deterministic).
	switch strings.ToLower(strings.TrimSpace(wf.Kind)) {
	case "path_intake":
		switch strings.ToLower(strings.TrimSpace(wf.Step)) {
		case "confirm_paths":
			if looksLikeHardConfirmation(userContent) {
				return firstWorkflowActionByIDPrefix(wf, "confirm_paths"), true
			}
		}
	}

	return workflowV1ActionMeta{}, false
}

func firstWorkflowActionByIDPrefix(wf *workflowV1Meta, prefix string) workflowV1ActionMeta {
	if wf == nil || strings.TrimSpace(prefix) == "" {
		return workflowV1ActionMeta{}
	}
	for _, a := range wf.Actions {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(a.ID)), strings.ToLower(strings.TrimSpace(prefix))) {
			return a
		}
	}
	// Fallback: return the first action so the caller's "true" result still yields a deterministic action.
	if len(wf.Actions) > 0 {
		return wf.Actions[0]
	}
	return workflowV1ActionMeta{}
}

func workflowTokenMatches(userContent string, token string) bool {
	token = strings.ToLower(strings.TrimSpace(token))
	if token == "" {
		return false
	}
	s := stripLeadingFiller(userContent)
	if s == "" {
		return false
	}
	ls := strings.ToLower(s)
	if ls == token {
		return true
	}
	if !strings.HasPrefix(ls, token) {
		return false
	}
	if len(ls) <= len(token) {
		return true
	}
	next := rune(ls[len(token)])
	return unicode.IsSpace(next) || strings.ContainsRune(".,;:!?)\"]}", next)
}

func stripLeadingFiller(content string) string {
	s := strings.TrimSpace(content)
	for {
		s = strings.TrimLeft(s, " \t\r\n,.;:!\"'")
		if s == "" {
			return ""
		}
		ls := strings.ToLower(s)
		if strings.HasPrefix(ls, "#") {
			s = strings.TrimSpace(s[1:])
			continue
		}

		trimmed := strings.TrimLeft(ls, " \t\r\n,.;:!\"'")
		if trimmed != ls {
			s = strings.TrimSpace(s[len(ls)-len(trimmed):])
			ls = strings.ToLower(s)
		}

		removed := false
		for _, w := range []string{"ok", "okay", "sure", "yes", "yeah", "yep"} {
			if strings.HasPrefix(ls, w) {
				if len(ls) == len(w) {
					s = ""
					removed = true
					break
				}
				next := rune(ls[len(w)])
				if unicode.IsSpace(next) || strings.ContainsRune(".,;:!?)\"]}", next) {
					s = strings.TrimSpace(s[len(w):])
					removed = true
					break
				}
			}
		}
		if !removed {
			break
		}
	}
	return strings.TrimSpace(s)
}

func looksLikeKeepTogether(content string) bool {
	s := strings.ToLower(strings.TrimSpace(content))
	return strings.Contains(s, "keep together") || strings.Contains(s, "keep it together")
}

func looksLikeHardConfirmation(content string) bool {
	s := strings.ToLower(strings.TrimSpace(content))
	if s == "" {
		return false
	}
	if strings.Contains(s, "?") {
		return false
	}
	if s == "no" || strings.HasPrefix(s, "no ") || strings.HasPrefix(s, "nah") || strings.HasPrefix(s, "nope") {
		return false
	}
	if looksLikeKeepTogether(s) {
		return false
	}
	if strings.Contains(s, "confirm") {
		return true
	}
	if strings.Contains(s, "ok") || strings.Contains(s, "okay") || strings.Contains(s, "sure") || strings.Contains(s, "sounds good") || strings.Contains(s, "that works") || strings.Contains(s, "fine") {
		return true
	}
	if strings.Contains(s, "go ahead") || strings.Contains(s, "do it") || strings.Contains(s, "proceed") || strings.Contains(s, "continue") {
		return true
	}
	// Numeric option tokens in a hard-sep prompt are treated as "accept recommendation".
	trim := stripLeadingFiller(s)
	return trim == "1" || trim == "2"
}
