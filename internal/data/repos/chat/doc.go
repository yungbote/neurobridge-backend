package chat

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type ChatDocRepo interface {
	Upsert(dbc dbctx.Context, rows []*types.ChatDoc) error
	GetByIDs(dbc dbctx.Context, userID uuid.UUID, ids []uuid.UUID) ([]*types.ChatDoc, error)
	LexicalSearch(dbc dbctx.Context, q ChatLexicalQuery) ([]*types.ChatDoc, error)
	LexicalSearchHits(dbc dbctx.Context, q ChatLexicalQuery) ([]ChatLexicalHit, error)
}

type chatDocRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewChatDocRepo(db *gorm.DB, log *logger.Logger) ChatDocRepo {
	return &chatDocRepo{
		db:  db,
		log: log.With("repo", "ChatDocRepo"),
	}
}

func (r *chatDocRepo) Upsert(dbc dbctx.Context, rows []*types.ChatDoc) error {
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
	return transaction.WithContext(dbc.Ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"doc_type",
			"scope",
			"scope_id",
			"thread_id",
			"path_id",
			"job_id",
			"source_id",
			"source_seq",
			"chunk_index",
			"text",
			"contextual_text",
			"embedding",
			"vector_id",
			"updated_at",
		}),
	}).Create(&rows).Error
}

func (r *chatDocRepo) GetByIDs(dbc dbctx.Context, userID uuid.UUID, ids []uuid.UUID) ([]*types.ChatDoc, error) {
	if userID == uuid.Nil {
		return nil, fmt.Errorf("missing user_id")
	}
	if len(ids) == 0 {
		return []*types.ChatDoc{}, nil
	}
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}
	var out []*types.ChatDoc
	if err := transaction.WithContext(dbc.Ctx).
		Model(&types.ChatDoc{}).
		Where("user_id = ? AND id IN ?", userID, ids).
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

type ChatLexicalQuery struct {
	UserID   uuid.UUID
	Scope    string
	ScopeID  *uuid.UUID
	DocTypes []string
	Query    string
	Limit    int
}

type ChatLexicalHit struct {
	Doc  *types.ChatDoc
	Rank float64
}

func (r *chatDocRepo) LexicalSearch(dbc dbctx.Context, q ChatLexicalQuery) ([]*types.ChatDoc, error) {
	hits, err := r.LexicalSearchHits(dbc, q)
	if err != nil {
		return nil, err
	}
	out := make([]*types.ChatDoc, 0, len(hits))
	for _, h := range hits {
		if h.Doc != nil {
			out = append(out, h.Doc)
		}
	}
	return out, nil
}

func (r *chatDocRepo) LexicalSearchHits(dbc dbctx.Context, q ChatLexicalQuery) ([]ChatLexicalHit, error) {
	if q.UserID == uuid.Nil {
		return nil, fmt.Errorf("missing user_id")
	}
	if strings.TrimSpace(q.Query) == "" {
		return []ChatLexicalHit{}, nil
	}
	if q.Limit <= 0 || q.Limit > 100 {
		q.Limit = 40
	}
	if strings.TrimSpace(q.Scope) == "" {
		return nil, fmt.Errorf("missing scope")
	}

	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}

	where := "chat_doc.user_id = ? AND chat_doc.scope = ?"
	args := []any{q.UserID, q.Scope}

	if q.ScopeID != nil && *q.ScopeID != uuid.Nil {
		where += " AND chat_doc.scope_id = ?"
		args = append(args, *q.ScopeID)
	} else {
		where += " AND chat_doc.scope_id IS NULL"
	}

	if len(q.DocTypes) > 0 {
		where += " AND chat_doc.doc_type IN ?"
		args = append(args, q.DocTypes)
	}

	// Use plainto_tsquery for safety. We rank by ts_rank and then recency.
	sql := fmt.Sprintf(`
		SELECT chat_doc.*,
		       ts_rank(to_tsvector('english', chat_doc.contextual_text), plainto_tsquery('english', ?)) AS rank
		FROM chat_doc
		WHERE %s
			AND to_tsvector('english', chat_doc.contextual_text) @@ plainto_tsquery('english', ?)
		ORDER BY rank DESC,
		         chat_doc.created_at DESC
		LIMIT %d;
	`, where, q.Limit)
	args = append(args, q.Query, q.Query)

	type row struct {
		types.ChatDoc
		Rank float64 `gorm:"column:rank"`
	}
	var rows []row
	if err := transaction.WithContext(dbc.Ctx).Raw(sql, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}

	out := make([]ChatLexicalHit, 0, len(rows))
	for i := range rows {
		d := rows[i].ChatDoc
		out = append(out, ChatLexicalHit{Doc: &d, Rank: rows[i].Rank})
	}
	return out, nil
}

func ParseEmbeddingJSON(b []byte) ([]float32, error) {
	if len(b) == 0 {
		return nil, nil
	}
	var v []float32
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	return v, nil
}

func MustEmbeddingJSON(v []float32) []byte {
	b, _ := json.Marshal(v)
	return b
}
