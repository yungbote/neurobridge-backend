package rollback

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

func PauseJob(ctx context.Context, repo interface {
	UpdateFields(dbc dbctx.Context, id uuid.UUID, updates map[string]interface{}) error
}, job *types.JobRun, reason string) {
	if job == nil || job.ID == uuid.Nil || repo == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	now := time.Now().UTC()
	msg := strings.TrimSpace(reason)
	if msg == "" {
		msg = "Paused: structural rollback active"
	}
	_ = repo.UpdateFields(dbctx.Context{Ctx: ctx}, job.ID, map[string]interface{}{
		"status":       "paused",
		"stage":        "structural_freeze",
		"message":      msg,
		"locked_at":    nil,
		"heartbeat_at": now,
		"updated_at":   now,
	})
}

func ResumePausedJobs(ctx context.Context, db *gorm.DB, stage string) (int, error) {
	if db == nil {
		return 0, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	stage = strings.TrimSpace(stage)
	if stage == "" {
		stage = "structural_freeze"
	}
	res := db.WithContext(ctx).Model(&types.JobRun{}).
		Where("status = ? AND stage = ?", "paused", stage).
		Updates(map[string]any{
			"status":     "queued",
			"stage":      "queued",
			"message":    "",
			"updated_at": time.Now().UTC(),
		})
	if res.Error != nil {
		return 0, res.Error
	}
	return int(res.RowsAffected), nil
}
