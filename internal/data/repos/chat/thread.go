package chat

import (
	"context"
	"fmt"
	"time"
	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
)

type ChatThreadRepo interface {
	Create(ctx context.Context, tx *gorm.DB, rows []*types.ChatThread) ([]*types.ChatThread, error)
	GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.ChatThread, error)
	ListByUser(ctx context.Context, tx *gorm.DB, userID uuid.UUID, limit int) ([]*types.ChatThread, error)
	LockByID(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*types.ChatThread, error)
	UpdateFields(ctx context.Context, tx *gorm.DB, id uuid.UUID, updates map[string]interface{}) error
}

type chatThreadRepo struct {
	db				*gorm.DB
	log				*logger.Logger
}

func NewChatThreadRepo(db *gorm.DB, log *logger.Logger) ChatThreadRepo {
	return &chatThreadRepo{db: db, log: log.With("repo", "ChatThreadRepo")}
}

func (r *chatThreadRepo) Create(ctx context.Context, tx *gorm.DB, rows []*types.ChatThread) ([]*types.ChatThread, error) {
	if len(rows) == 0 { return []*types.ChatThread{}, nil }
	txx := tx
	if txx == nil { txx = r.db }
	if err := txx.WithContext(ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *chatThreadRepo) GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.ChatThread, error) {
	if len(ids) == 0 { return []*types.ChatThread{}, nil }
	txx := tx
	if txx == nil { txx = r.db }
	var out []*types.ChatThread
	if err := txx.WithContext(ctx).
		Model(&types.ChatThread{}).
		Where("id IN ?", ids).
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *chatThreadRepo) ListByUser(ctx context.Context, tx *gorm.DB, userID uuid.UUID, limit int) ([]*types.ChatThread, error) {
	if userID == uuid.Nil { return nil, fmt.Errorf("missing user_id") }
	if limit <= 0 || limit > 200 { limit = 50 }
	txx := tx
	if txx == nil { txx = r.db }
	var out []*types.ChatThread
	if err := txx.WithContext(ctx).
		Model(&types.ChatThread{}).
		Where("user_id = ? AND status = ?", userID, "active").
		Order("updated_at DESC").
		Limit(limit).
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *chatThreadRepo) LockByID(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*types.ChatThread, error) {
	if id == uuid.Nil { return nil, fmt.Errorf("missing id") }
	if tx == nil { return nil, fmt.Errorf("LockByID required tx") }
	var out types.ChatThread
	if err := tx.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ?", id).
		Take(&out).Error; err != nil {
		return nil, err
	}
	return &out, nil
}

func (r *chatThreadRepo) UpdateFields(ctx context.Context, tx *gorm.DB, id uuid.UUID, updates map[string]interface{}) error {
	if id == uuid.Nil { return fmt.Errorf("missing id") }
	if updates == nil { updates = map[string]interface{}{} }
	updates["updated_at"] = time.Now().UTC()
	txx := tx
	if txx == nil { txx = r.db }
	return txx.WithContext(ctx).
		Model(&types.ChatThread{}).
		Where("id = ?", id).
		Updates(updates).Error
}









