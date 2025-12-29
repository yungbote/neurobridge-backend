package chat

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type ChatThreadStateRepo interface {
	GetByThreadID(dbc dbctx.Context, threadID uuid.UUID) (*types.ChatThreadState, error)
	GetOrCreate(dbc dbctx.Context, threadID uuid.UUID) (*types.ChatThreadState, error)
	UpdateFields(dbc dbctx.Context, threadID uuid.UUID, updates map[string]interface{}) error
}

type chatThreadStateRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewChatThreadStateRepo(db *gorm.DB, log *logger.Logger) ChatThreadStateRepo {
	return &chatThreadStateRepo{
		db:  db,
		log: log.With("repo", "ChatThreadStateRepo"),
	}
}

func (r *chatThreadStateRepo) GetByThreadID(dbc dbctx.Context, threadID uuid.UUID) (*types.ChatThreadState, error) {
	if threadID == uuid.Nil {
		return nil, fmt.Errorf("missing thread_id")
	}
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}
	var out types.ChatThreadState
	err := transaction.WithContext(dbc.Ctx).Where("thread_id = ?", threadID).First(&out).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (r *chatThreadStateRepo) GetOrCreate(dbc dbctx.Context, threadID uuid.UUID) (*types.ChatThreadState, error) {
	if threadID == uuid.Nil {
		return nil, fmt.Errorf("missing thread_id")
	}
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}
	ex, err := r.GetByThreadID(dbctx.Context{Ctx: dbc.Ctx, Tx: transaction}, threadID)
	if err != nil {
		return nil, err
	}
	if ex != nil {
		return ex, nil
	}

	now := time.Now().UTC()
	row := &types.ChatThreadState{
		ThreadID:          threadID,
		LastIndexedSeq:    0,
		LastSummarizedSeq: 0,
		LastGraphSeq:      0,
		LastMemorySeq:     0,
		UpdatedAt:         now,
	}

	if err := transaction.WithContext(dbc.Ctx).Create(row).Error; err != nil {
		// Possible race: another worker created it.
		ex2, getErr := r.GetByThreadID(dbctx.Context{Ctx: dbc.Ctx, Tx: transaction}, threadID)
		if getErr != nil {
			return nil, err
		}
		if ex2 != nil {
			return ex2, nil
		}
		return nil, err
	}
	return row, nil
}

func (r *chatThreadStateRepo) UpdateFields(dbc dbctx.Context, threadID uuid.UUID, updates map[string]interface{}) error {
	if threadID == uuid.Nil {
		return fmt.Errorf("missing thread_id")
	}
	if updates == nil {
		updates = map[string]interface{}{}
	}

	// Make cursor updates monotonic to tolerate concurrent/out-of-order maintenance jobs.
	if v, ok := updates["last_indexed_seq"]; ok {
		updates["last_indexed_seq"] = gorm.Expr("GREATEST(last_indexed_seq, ?)", v)
	}
	if v, ok := updates["last_summarized_seq"]; ok {
		updates["last_summarized_seq"] = gorm.Expr("GREATEST(last_summarized_seq, ?)", v)
	}
	if v, ok := updates["last_graph_seq"]; ok {
		updates["last_graph_seq"] = gorm.Expr("GREATEST(last_graph_seq, ?)", v)
	}
	if v, ok := updates["last_memory_seq"]; ok {
		updates["last_memory_seq"] = gorm.Expr("GREATEST(last_memory_seq, ?)", v)
	}

	updates["updated_at"] = time.Now().UTC()

	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}
	return transaction.WithContext(dbc.Ctx).
		Model(&types.ChatThreadState{}).
		Where("thread_id = ?", threadID).
		Updates(updates).Error
}
