package chat

import (
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

type ChatEntityRepo interface {
	UpsertByName(dbc dbctx.Context, row *types.ChatEntity) (*types.ChatEntity, error)
}

type ChatEdgeRepo interface {
	Create(dbc dbctx.Context, rows []*types.ChatEdge) error
}

type ChatClaimRepo interface {
	InsertIgnore(dbc dbctx.Context, rows []*types.ChatClaim) error
}

type chatEntityRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewChatEntityRepo(db *gorm.DB, log *logger.Logger) ChatEntityRepo {
	return &chatEntityRepo{
		db:  db,
		log: log.With("repo", "ChatEntityRepo"),
	}
}

func (r *chatEntityRepo) UpsertByName(dbc dbctx.Context, row *types.ChatEntity) (*types.ChatEntity, error) {
	if row == nil || row.UserID == uuid.Nil {
		return nil, fmt.Errorf("missing row/user_id")
	}
	name := strings.TrimSpace(row.Name)
	if name == "" {
		return nil, fmt.Errorf("missing entity name")
	}

	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}

	q := transaction.WithContext(dbc.Ctx).
		Model(&types.ChatEntity{}).
		Where("user_id = ? AND scope = ? AND lower(name) = lower(?)", row.UserID, strings.TrimSpace(row.Scope), name)
	if row.ScopeID != nil && *row.ScopeID != uuid.Nil {
		q = q.Where("scope_id = ?", *row.ScopeID)
	} else {
		q = q.Where("scope_id IS NULL")
	}

	var existing types.ChatEntity
	err := q.First(&existing).Error

	now := time.Now().UTC()
	row.UpdatedAt = now

	if err == nil && existing.ID != uuid.Nil {
		row.ID = existing.ID
		if err := transaction.WithContext(dbc.Ctx).
			Model(&types.ChatEntity{}).
			Where("id = ?", existing.ID).
			Updates(map[string]interface{}{
				"type":        row.Type,
				"description": row.Description,
				"aliases":     row.Aliases,
				"thread_id":   row.ThreadID,
				"path_id":     row.PathID,
				"job_id":      row.JobID,
				"updated_at":  row.UpdatedAt,
			}).Error; err != nil {
			return nil, err
		}
		return row, nil
	}
	if err != nil && err != gorm.ErrRecordNotFound {
		return nil, err
	}

	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	if err := transaction.WithContext(dbc.Ctx).Create(row).Error; err != nil {
		return nil, err
	}
	return row, nil
}

type chatEdgeRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewChatEdgeRepo(db *gorm.DB, log *logger.Logger) ChatEdgeRepo {
	return &chatEdgeRepo{
		db:  db,
		log: log.With("repo", "ChatEdgeRepo"),
	}
}

func (r *chatEdgeRepo) Create(dbc dbctx.Context, rows []*types.ChatEdge) error {
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
		if row.CreatedAt.IsZero() {
			row.CreatedAt = now
		}
	}
	// Idempotent insert: edges are derived and may be re-created by retries.
	return transaction.WithContext(dbc.Ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&rows).Error
}

type chatClaimRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewChatClaimRepo(db *gorm.DB, log *logger.Logger) ChatClaimRepo {
	return &chatClaimRepo{
		db:  db,
		log: log.With("repo", "ChatClaimRepo"),
	}
}

func (r *chatClaimRepo) InsertIgnore(dbc dbctx.Context, rows []*types.ChatClaim) error {
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
		if row.CreatedAt.IsZero() {
			row.CreatedAt = now
		}
	}
	return transaction.WithContext(dbc.Ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&rows).Error
}
