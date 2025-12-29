package chat

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type ChatMessageRepo interface {
	Create(dbc dbctx.Context, rows []*types.ChatMessage) ([]*types.ChatMessage, error)
	GetMaxSeq(dbc dbctx.Context, threadID uuid.UUID) (int64, error)
	ListRecent(dbc dbctx.Context, threadID uuid.UUID, limit int) ([]*types.ChatMessage, error)
	ListByThread(dbc dbctx.Context, threadID uuid.UUID, limit int) ([]*types.ChatMessage, error)
	ListSinceSeq(dbc dbctx.Context, threadID uuid.UUID, afterSeq int64, limit int) ([]*types.ChatMessage, error)
	// LexicalSearchHits provides a SQL-only fallback when projections are empty or external indexes are degraded.
	LexicalSearchHits(dbc dbctx.Context, q ChatMessageLexicalQuery) ([]ChatMessageLexicalHit, error)
	UpdateFields(dbc dbctx.Context, id uuid.UUID, updates map[string]interface{}) error
}

type chatMessageRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewChatMessageRepo(db *gorm.DB, log *logger.Logger) ChatMessageRepo {
	return &chatMessageRepo{db: db, log: log.With("repo", "ChatMessageRepo")}
}

func (r *chatMessageRepo) Create(dbc dbctx.Context, rows []*types.ChatMessage) ([]*types.ChatMessage, error) {
	if len(rows) == 0 {
		return []*types.ChatMessage{}, nil
	}
	txx := dbc.Tx
	if txx == nil {
		txx = r.db
	}
	if err := txx.WithContext(dbc.Ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *chatMessageRepo) GetMaxSeq(dbc dbctx.Context, threadID uuid.UUID) (int64, error) {
	if threadID == uuid.Nil {
		return 0, fmt.Errorf("missing thread_id")
	}
	txx := dbc.Tx
	if txx == nil {
		txx = r.db
	}
	var maxSeq int64
	if err := txx.WithContext(dbc.Ctx).
		Model(&types.ChatMessage{}).
		Select("COALESCE(MAX(seq), 0)").
		Where("thread_id = ?", threadID).
		Scan(&maxSeq).Error; err != nil {
		return 0, err
	}
	return maxSeq, nil
}

func (r *chatMessageRepo) ListRecent(dbc dbctx.Context, threadID uuid.UUID, limit int) ([]*types.ChatMessage, error) {
	if threadID == uuid.Nil {
		return nil, fmt.Errorf("missing thread_id")
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	txx := dbc.Tx
	if txx == nil {
		txx = r.db
	}
	var out []*types.ChatMessage
	if err := txx.WithContext(dbc.Ctx).
		Model(&types.ChatMessage{}).
		Where("thread_id = ?", threadID).
		Order("seq DESC").
		Limit(limit).
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *chatMessageRepo) ListByThread(dbc dbctx.Context, threadID uuid.UUID, limit int) ([]*types.ChatMessage, error) {
	if threadID == uuid.Nil {
		return nil, fmt.Errorf("missing thread_id")
	}
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	txx := dbc.Tx
	if txx == nil {
		txx = r.db
	}
	var out []*types.ChatMessage
	if err := txx.WithContext(dbc.Ctx).
		Model(&types.ChatMessage{}).
		Where("thread_id = ?", threadID).
		Order("seq DESC").
		Limit(limit).
		Find(&out).Error; err != nil {
		return nil, err
	}
	// Normalize to ASC for clients.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

func (r *chatMessageRepo) ListSinceSeq(dbc dbctx.Context, threadID uuid.UUID, afterSeq int64, limit int) ([]*types.ChatMessage, error) {
	if threadID == uuid.Nil {
		return nil, fmt.Errorf("missing thread_id")
	}
	if limit <= 0 || limit > 1000 {
		limit = 300
	}
	txx := dbc.Tx
	if txx == nil {
		txx = r.db
	}
	var out []*types.ChatMessage
	if err := txx.WithContext(dbc.Ctx).
		Model(&types.ChatMessage{}).
		Where("thread_id = ? AND seq > ?", threadID, afterSeq).
		Order("seq ASC").
		Limit(limit).
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

type ChatMessageLexicalQuery struct {
	UserID   uuid.UUID
	ThreadID uuid.UUID
	Query    string
	Limit    int
}

type ChatMessageLexicalHit struct {
	Msg  *types.ChatMessage
	Rank float64
}

func (r *chatMessageRepo) LexicalSearchHits(dbc dbctx.Context, q ChatMessageLexicalQuery) ([]ChatMessageLexicalHit, error) {
	if q.UserID == uuid.Nil {
		return nil, fmt.Errorf("missing user_id")
	}
	if q.ThreadID == uuid.Nil {
		return nil, fmt.Errorf("missing thread_id")
	}
	if strings.TrimSpace(q.Query) == "" {
		return []ChatMessageLexicalHit{}, nil
	}
	if q.Limit <= 0 || q.Limit > 100 {
		q.Limit = 30
	}

	txx := dbc.Tx
	if txx == nil {
		txx = r.db
	}

	sql := fmt.Sprintf(`
		SELECT chat_message.*,
		       ts_rank(to_tsvector('english', chat_message.content), plainto_tsquery('english', ?)) AS rank
		FROM chat_message
		WHERE chat_message.user_id = ?
		  AND chat_message.thread_id = ?
		  AND chat_message.deleted_at IS NULL
		  AND to_tsvector('english', chat_message.content) @@ plainto_tsquery('english', ?)
		ORDER BY rank DESC, chat_message.seq DESC
		LIMIT %d;
	`, q.Limit)

	type row struct {
		types.ChatMessage
		Rank float64 `gorm:"column:rank"`
	}
	var rows []row
	if err := txx.WithContext(dbc.Ctx).Raw(sql, q.Query, q.UserID, q.ThreadID, q.Query).Scan(&rows).Error; err != nil {
		return nil, err
	}

	out := make([]ChatMessageLexicalHit, 0, len(rows))
	for i := range rows {
		m := rows[i].ChatMessage
		out = append(out, ChatMessageLexicalHit{Msg: &m, Rank: rows[i].Rank})
	}
	return out, nil
}

func (r *chatMessageRepo) UpdateFields(dbc dbctx.Context, id uuid.UUID, updates map[string]interface{}) error {
	if id == uuid.Nil {
		return fmt.Errorf("missing_id")
	}
	if updates == nil {
		updates = map[string]interface{}{}
	}
	updates["updated_at"] = time.Now().UTC()
	txx := dbc.Tx
	if txx == nil {
		txx = r.db
	}
	return txx.WithContext(dbc.Ctx).
		Model(&types.ChatMessage{}).
		Where("id = ?", id).
		Updates(updates).Error
}
