package chat

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type ChatSummaryNodeRepo interface {
	Create(ctx context.Context, tx *gorm.DB, rows []*types.ChatSummaryNode) error
	ListOrphansByLevel(ctx context.Context, tx *gorm.DB, threadID uuid.UUID, level int) ([]*types.ChatSummaryNode, error)
	SetParent(ctx context.Context, tx *gorm.DB, childIDs []uuid.UUID, parentID uuid.UUID) error
	GetRoot(ctx context.Context, tx *gorm.DB, threadID uuid.UUID) (*types.ChatSummaryNode, error)
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

func (r *chatSummaryNodeRepo) Create(ctx context.Context, tx *gorm.DB, rows []*types.ChatSummaryNode) error {
	if len(rows) == 0 {
		return nil
	}
	transaction := tx
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
	return transaction.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&rows).Error
}

func (r *chatSummaryNodeRepo) ListOrphansByLevel(ctx context.Context, tx *gorm.DB, threadID uuid.UUID, level int) ([]*types.ChatSummaryNode, error) {
	if threadID == uuid.Nil {
		return nil, fmt.Errorf("missing thread_id")
	}
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}
	var out []*types.ChatSummaryNode
	if err := transaction.WithContext(ctx).
		Model(&types.ChatSummaryNode{}).
		Where("thread_id = ? AND level = ? AND parent_id IS NULL", threadID, level).
		Order("start_seq ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *chatSummaryNodeRepo) SetParent(ctx context.Context, tx *gorm.DB, childIDs []uuid.UUID, parentID uuid.UUID) error {
	if len(childIDs) == 0 || parentID == uuid.Nil {
		return nil
	}
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}
	return transaction.WithContext(ctx).
		Model(&types.ChatSummaryNode{}).
		Where("id IN ?", childIDs).
		Updates(map[string]interface{}{
			"parent_id":  parentID,
			"updated_at": time.Now().UTC(),
		}).Error
}

func (r *chatSummaryNodeRepo) GetRoot(ctx context.Context, tx *gorm.DB, threadID uuid.UUID) (*types.ChatSummaryNode, error) {
	if threadID == uuid.Nil {
		return nil, fmt.Errorf("missing thread_id")
	}
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}
	var out types.ChatSummaryNode
	err := transaction.WithContext(ctx).
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
