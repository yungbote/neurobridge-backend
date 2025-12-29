package chat

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type ChatThreadRepo interface {
	Create(dbc dbctx.Context, rows []*types.ChatThread) ([]*types.ChatThread, error)
	GetByIDs(dbc dbctx.Context, ids []uuid.UUID) ([]*types.ChatThread, error)
	ListByUser(dbc dbctx.Context, userID uuid.UUID, limit int) ([]*types.ChatThread, error)
	LockByID(dbc dbctx.Context, id uuid.UUID) (*types.ChatThread, error)
	UpdateFields(dbc dbctx.Context, id uuid.UUID, updates map[string]interface{}) error
}

type chatThreadRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewChatThreadRepo(db *gorm.DB, log *logger.Logger) ChatThreadRepo {
	return &chatThreadRepo{db: db, log: log.With("repo", "ChatThreadRepo")}
}

func (r *chatThreadRepo) Create(dbc dbctx.Context, rows []*types.ChatThread) ([]*types.ChatThread, error) {
	if len(rows) == 0 {
		return []*types.ChatThread{}, nil
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

func (r *chatThreadRepo) GetByIDs(dbc dbctx.Context, ids []uuid.UUID) ([]*types.ChatThread, error) {
	if len(ids) == 0 {
		return []*types.ChatThread{}, nil
	}
	txx := dbc.Tx
	if txx == nil {
		txx = r.db
	}
	var out []*types.ChatThread
	if err := txx.WithContext(dbc.Ctx).
		Model(&types.ChatThread{}).
		Where("id IN ?", ids).
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *chatThreadRepo) ListByUser(dbc dbctx.Context, userID uuid.UUID, limit int) ([]*types.ChatThread, error) {
	if userID == uuid.Nil {
		return nil, fmt.Errorf("missing user_id")
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	txx := dbc.Tx
	if txx == nil {
		txx = r.db
	}
	var out []*types.ChatThread
	if err := txx.WithContext(dbc.Ctx).
		Model(&types.ChatThread{}).
		Where("user_id = ? AND status = ?", userID, "active").
		Order("updated_at DESC").
		Limit(limit).
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *chatThreadRepo) LockByID(dbc dbctx.Context, id uuid.UUID) (*types.ChatThread, error) {
	if id == uuid.Nil {
		return nil, fmt.Errorf("missing id")
	}
	if dbc.Tx == nil {
		return nil, fmt.Errorf("LockByID required dbc.Tx")
	}
	var out types.ChatThread
	if err := dbc.Tx.WithContext(dbc.Ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ?", id).
		Take(&out).Error; err != nil {
		return nil, err
	}
	return &out, nil
}

func (r *chatThreadRepo) UpdateFields(dbc dbctx.Context, id uuid.UUID, updates map[string]interface{}) error {
	if id == uuid.Nil {
		return fmt.Errorf("missing id")
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
		Model(&types.ChatThread{}).
		Where("id = ?", id).
		Updates(updates).Error
}
