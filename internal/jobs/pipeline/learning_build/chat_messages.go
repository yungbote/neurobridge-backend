package learning_build

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
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
)

func (p *Pipeline) maybeAppendPathBuildReadyMessage(jc *jobrt.Context, materialSetID, pathID uuid.UUID) {
	if p == nil || jc == nil || jc.Job == nil || p.db == nil || p.threads == nil || p.messages == nil {
		return
	}
	threadID, ok := jc.PayloadUUID("thread_id")
	if !ok || threadID == uuid.Nil {
		return
	}
	if pathID == uuid.Nil {
		return
	}

	owner := jc.Job.OwnerUserID
	jobID := jc.Job.ID
	if owner == uuid.Nil || jobID == uuid.Nil {
		return
	}

	title := "Your path is ready"
	desc := ""
	if p.path != nil {
		if row, err := p.path.GetByID(dbctx.Context{Ctx: jc.Ctx, Tx: p.db}, pathID); err == nil && row != nil {
			if s := strings.TrimSpace(row.Title); s != "" {
				title = s
			}
			desc = strings.TrimSpace(row.Description)
		}
	}

	nodeCount := int64(0)
	actCount := int64(0)
	conceptCount := int64(0)
	_ = p.db.WithContext(jc.Ctx).Model(&types.PathNode{}).Where("path_id = ?", pathID).Count(&nodeCount).Error
	_ = p.db.WithContext(jc.Ctx).
		Model(&types.PathNodeActivity{}).
		Joins("JOIN path_node ON path_node_activity.path_node_id = path_node.id").
		Where("path_node.path_id = ?", pathID).
		Count(&actCount).Error
	_ = p.db.WithContext(jc.Ctx).
		Model(&types.Concept{}).
		Where("scope = ? AND scope_id = ?", "path", pathID).
		Count(&conceptCount).Error

	stats := map[string]any{
		"node_count":     nodeCount,
		"activity_count": actCount,
		"concept_count":  conceptCount,
	}

	lines := []string{
		fmt.Sprintf("**%s**", title),
	}
	if desc != "" {
		lines = append(lines, desc)
	}

	countBits := make([]string, 0, 3)
	if nodeCount > 0 {
		countBits = append(countBits, fmt.Sprintf("%d units", nodeCount))
	}
	if actCount > 0 {
		countBits = append(countBits, fmt.Sprintf("%d activities", actCount))
	}
	if conceptCount > 0 {
		countBits = append(countBits, fmt.Sprintf("%d concepts", conceptCount))
	}
	if len(countBits) > 0 {
		lines = append(lines, strings.Join(countBits, " • "))
	}
	lines = append(lines, "Click the card below to open it.")
	content := strings.TrimSpace(strings.Join(lines, "\n\n"))

	meta := map[string]any{
		"kind":            "path_ready",
		"job_id":          jobID.String(),
		"path_id":         pathID.String(),
		"material_set_id": materialSetID.String(),
		"stats":           stats,
		"card": map[string]any{
			"type":    "path",
			"path_id": pathID.String(),
		},
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
			return nil
		}

		// Idempotency: if we've already posted for this job, skip.
		var existing types.ChatMessage
		e := tx.WithContext(jc.Ctx).
			Model(&types.ChatMessage{}).
			Where("thread_id = ? AND user_id = ? AND metadata->>'kind' = ? AND metadata->>'job_id' = ?", threadID, owner, "path_ready", jobID.String()).
			First(&existing).Error
		if e == nil && existing.ID != uuid.Nil {
			return nil
		}
		if e != nil && e != gorm.ErrRecordNotFound {
			return e
		}

		now := time.Now().UTC()
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

		threadUpdates := map[string]interface{}{
			"next_seq":        nextSeq,
			"last_message_at": now,
			"updated_at":      now,
		}
		if strings.EqualFold(strings.TrimSpace(th.Title), "new chat") && strings.TrimSpace(title) != "" {
			threadUpdates["title"] = title
		}
		if err := p.threads.UpdateFields(inner, threadID, threadUpdates); err != nil {
			return err
		}

		created = msg
		createdNew = true
		return nil
	})
	if err != nil {
		if p.log != nil {
			p.log.Warn("Failed to append path_ready chat message", "error", err, "thread_id", threadID.String(), "path_id", pathID.String())
		}
		return
	}

	if createdNew && created != nil && p.chatNotif != nil {
		p.chatNotif.MessageCreated(owner, threadID, created, nil)
	}
}

func (p *Pipeline) maybeAppendPathBuildFailedMessage(jc *jobrt.Context, pathID uuid.UUID) {
	if p == nil || jc == nil || jc.Job == nil || p.db == nil || p.threads == nil || p.messages == nil {
		return
	}
	threadID, ok := jc.PayloadUUID("thread_id")
	if !ok || threadID == uuid.Nil {
		return
	}
	owner := jc.Job.OwnerUserID
	jobID := jc.Job.ID
	if owner == uuid.Nil || jobID == uuid.Nil {
		return
	}
	if pathID == uuid.Nil {
		return
	}

	errText := strings.TrimSpace(jc.Job.Error)
	if errText == "" {
		errText = "Unknown error"
	}
	content := strings.TrimSpace(strings.Join([]string{
		"Generation failed.",
		errText,
	}, "\n\n"))

	meta := map[string]any{
		"kind":    "path_generation_failed",
		"job_id":  jobID.String(),
		"path_id": pathID.String(),
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
			return nil
		}

		// Idempotency: only one failure message per job.
		var existing types.ChatMessage
		e := tx.WithContext(jc.Ctx).
			Model(&types.ChatMessage{}).
			Where("thread_id = ? AND user_id = ? AND metadata->>'kind' = ? AND metadata->>'job_id' = ?", threadID, owner, "path_generation_failed", jobID.String()).
			First(&existing).Error
		if e == nil && existing.ID != uuid.Nil {
			return nil
		}
		if e != nil && e != gorm.ErrRecordNotFound {
			return e
		}

		now := time.Now().UTC()
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
		if p.log != nil {
			p.log.Warn("Failed to append path_generation_failed chat message", "error", err, "thread_id", threadID.String(), "path_id", pathID.String())
		}
		return
	}
	if createdNew && created != nil && p.chatNotif != nil {
		p.chatNotif.MessageCreated(owner, threadID, created, nil)
	}
}
