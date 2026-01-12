package chat

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type ChatTurnRepo interface {
	Create(dbc dbctx.Context, row *types.ChatTurn) error
	GetByID(dbc dbctx.Context, userID uuid.UUID, turnID uuid.UUID) (*types.ChatTurn, error)
	GetByUserMessageID(dbc dbctx.Context, userID uuid.UUID, threadID uuid.UUID, userMessageID uuid.UUID) (*types.ChatTurn, error)
	UpdateFields(dbc dbctx.Context, userID uuid.UUID, turnID uuid.UUID, updates map[string]interface{}) error
}

type chatTurnRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewChatTurnRepo(db *gorm.DB, log *logger.Logger) ChatTurnRepo {
	return &chatTurnRepo{
		db:  db,
		log: log.With("repo", "ChatTurnRepo"),
	}
}

func (r *chatTurnRepo) Create(dbc dbctx.Context, row *types.ChatTurn) error {
	if row == nil || row.UserID == uuid.Nil || row.ThreadID == uuid.Nil || row.UserMessageID == uuid.Nil || row.AssistantMessageID == uuid.Nil {
		return fmt.Errorf("invalid chat turn")
	}
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}
	now := time.Now().UTC()
	row.UpdatedAt = now
	if row.CreatedAt.IsZero() {
		row.CreatedAt = now
	}
	if err := transaction.WithContext(dbc.Ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(row).Error; err != nil {
		return err
	}
	return nil
}

func (r *chatTurnRepo) GetByID(dbc dbctx.Context, userID uuid.UUID, turnID uuid.UUID) (*types.ChatTurn, error) {
	if userID == uuid.Nil || turnID == uuid.Nil {
		return nil, fmt.Errorf("missing ids")
	}
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}
	var out types.ChatTurn
	err := transaction.WithContext(dbc.Ctx).
		Model(&types.ChatTurn{}).
		Where("id = ? AND user_id = ?", turnID, userID).
		First(&out).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (r *chatTurnRepo) GetByUserMessageID(dbc dbctx.Context, userID uuid.UUID, threadID uuid.UUID, userMessageID uuid.UUID) (*types.ChatTurn, error) {
	if userID == uuid.Nil || threadID == uuid.Nil || userMessageID == uuid.Nil {
		return nil, fmt.Errorf("missing ids")
	}
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}
	var out types.ChatTurn
	err := transaction.WithContext(dbc.Ctx).
		Model(&types.ChatTurn{}).
		Where("user_id = ? AND thread_id = ? AND user_message_id = ?", userID, threadID, userMessageID).
		First(&out).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (r *chatTurnRepo) UpdateFields(dbc dbctx.Context, userID uuid.UUID, turnID uuid.UUID, updates map[string]interface{}) error {
	if userID == uuid.Nil || turnID == uuid.Nil {
		return fmt.Errorf("missing ids")
	}
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}
	if updates == nil {
		updates = map[string]interface{}{}
	}
	updates["updated_at"] = time.Now().UTC()
	return transaction.WithContext(dbc.Ctx).
		Model(&types.ChatTurn{}).
		Where("id = ? AND user_id = ?", turnID, userID).
		Updates(updates).Error
}
