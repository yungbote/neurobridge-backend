package chat

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type ChatSummaryNodeRepo interface {
	Create(dbc dbctx.Context, rows []*types.ChatSummaryNode) error
	ListOrphansByLevel(dbc dbctx.Context, threadID uuid.UUID, level int) ([]*types.ChatSummaryNode, error)
	SetParent(dbc dbctx.Context, childIDs []uuid.UUID, parentID uuid.UUID) error
	GetRoot(dbc dbctx.Context, threadID uuid.UUID) (*types.ChatSummaryNode, error)
}

type chatSummaryNodeRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewChatSummaryNodeRepo(db *gorm.DB, log *logger.Logger) ChatSummaryNodeRepo {
	return &chatSummaryNodeRepo{
		db:  db,
		log: log.With("repo", "ChatSummaryNodeRepo"),
	}
}

func (r *chatSummaryNodeRepo) Create(dbc dbctx.Context, rows []*types.ChatSummaryNode) error {
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
	}
	// Idempotent insert: summary nodes are derived and may be created concurrently by retries.
	return transaction.WithContext(dbc.Ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&rows).Error
}

func (r *chatSummaryNodeRepo) ListOrphansByLevel(dbc dbctx.Context, threadID uuid.UUID, level int) ([]*types.ChatSummaryNode, error) {
	if threadID == uuid.Nil {
		return nil, fmt.Errorf("missing thread_id")
	}
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}
	var out []*types.ChatSummaryNode
	if err := transaction.WithContext(dbc.Ctx).
		Model(&types.ChatSummaryNode{}).
		Where("thread_id = ? AND level = ? AND parent_id IS NULL", threadID, level).
		Order("start_seq ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *chatSummaryNodeRepo) SetParent(dbc dbctx.Context, childIDs []uuid.UUID, parentID uuid.UUID) error {
	if len(childIDs) == 0 || parentID == uuid.Nil {
		return nil
	}
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}
	return transaction.WithContext(dbc.Ctx).
		Model(&types.ChatSummaryNode{}).
		Where("id IN ?", childIDs).
		Updates(map[string]interface{}{
			"parent_id":  parentID,
			"updated_at": time.Now().UTC(),
		}).Error
}

func (r *chatSummaryNodeRepo) GetRoot(dbc dbctx.Context, threadID uuid.UUID) (*types.ChatSummaryNode, error) {
	if threadID == uuid.Nil {
		return nil, fmt.Errorf("missing thread_id")
	}
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}
	var out types.ChatSummaryNode
	err := transaction.WithContext(dbc.Ctx).
		Model(&types.ChatSummaryNode{}).
		Where("thread_id = ? AND parent_id IS NULL", threadID).
		Order("level DESC, end_seq DESC").
		First(&out).Error
	if err != nil {
		return nil, err
	}
	if out.ID == uuid.Nil {
		return nil, nil
	}
	return &out, nil
}
