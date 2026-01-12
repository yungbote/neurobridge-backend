package chat

import (
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type ChatMemoryItemRepo interface {
	UpsertMany(dbc dbctx.Context, rows []*types.ChatMemoryItem) error
}

type chatMemoryItemRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewChatMemoryItemRepo(db *gorm.DB, log *logger.Logger) ChatMemoryItemRepo {
	return &chatMemoryItemRepo{
		db:  db,
		log: log.With("repo", "ChatMemoryItemRepo"),
	}
}

func (r *chatMemoryItemRepo) UpsertMany(dbc dbctx.Context, rows []*types.ChatMemoryItem) error {
	if len(rows) == 0 {
		return nil
	}
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}

	now := time.Now().UTC()

	for _, row := range rows {
		if row == nil {
			continue
		}
		row.UpdatedAt = now
		if row.CreatedAt.IsZero() {
			row.CreatedAt = now
		}

		// Manual upsert to handle nullable scope_id safely (user scope uses NULL).
		q := transaction.WithContext(dbc.Ctx).Model(&types.ChatMemoryItem{}).
			Where("user_id = ? AND scope = ? AND kind = ? AND key = ? AND deleted_at IS NULL",
				row.UserID, strings.TrimSpace(row.Scope), strings.TrimSpace(row.Kind), strings.TrimSpace(row.Key),
			)
		if row.ScopeID != nil && *row.ScopeID != uuid.Nil {
			q = q.Where("scope_id = ?", *row.ScopeID)
		} else {
			q = q.Where("scope_id IS NULL")
		}

		var existing types.ChatMemoryItem
		err := q.First(&existing).Error
		if err == nil && existing.ID != uuid.Nil {
			if err := transaction.WithContext(dbc.Ctx).
				Model(&types.ChatMemoryItem{}).
				Where("id = ?", existing.ID).
				Updates(map[string]interface{}{
					"value":         row.Value,
					"confidence":    row.Confidence,
					"evidence_seqs": row.EvidenceSeqs,
					"thread_id":     row.ThreadID,
					"path_id":       row.PathID,
					"job_id":        row.JobID,
					"updated_at":    row.UpdatedAt,
				}).Error; err != nil {
				return err
			}
			row.ID = existing.ID
			continue
		}
		if err != nil && err != gorm.ErrRecordNotFound {
			return err
		}

		if row.ID == uuid.Nil {
			row.ID = uuid.New()
		}
		if err := transaction.WithContext(dbc.Ctx).Create(row).Error; err != nil {
			return err
		}
	}

	return nil
}
