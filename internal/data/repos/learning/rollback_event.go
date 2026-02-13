package learning

import (
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type RollbackEventRepo interface {
	Create(dbc dbctx.Context, row *types.RollbackEvent) error
	UpdateStatus(dbc dbctx.Context, id uuid.UUID, status string, completedAt *time.Time) error
}

type rollbackEventRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewRollbackEventRepo(db *gorm.DB, baseLog *logger.Logger) RollbackEventRepo {
	return &rollbackEventRepo{db: db, log: baseLog.With("repo", "RollbackEventRepo")}
}

func (r *rollbackEventRepo) Create(dbc dbctx.Context, row *types.RollbackEvent) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if row == nil {
		return nil
	}
	now := time.Now().UTC()
	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	if row.CreatedAt.IsZero() {
		row.CreatedAt = now
	}
	if row.InitiatedAt == nil {
		row.InitiatedAt = &now
	}
	return t.WithContext(dbc.Ctx).Create(row).Error
}

func (r *rollbackEventRepo) UpdateStatus(dbc dbctx.Context, id uuid.UUID, status string, completedAt *time.Time) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if id == uuid.Nil {
		return nil
	}
	status = strings.TrimSpace(status)
	updates := map[string]any{}
	if status != "" {
		updates["status"] = status
	}
	if completedAt != nil {
		updates["completed_at"] = *completedAt
	}
	if len(updates) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).
		Model(&types.RollbackEvent{}).
		Where("id = ?", id).
		Updates(updates).Error
}
