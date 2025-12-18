package learning

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type PathRepo interface {
	Create(ctx context.Context, tx *gorm.DB, rows []*types.Path) ([]*types.Path, error)

	GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.Path, error)
	GetByID(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*types.Path, error)

	ListByUser(ctx context.Context, tx *gorm.DB, userID *uuid.UUID) ([]*types.Path, error)
	ListByUserIDs(ctx context.Context, tx *gorm.DB, userIDs []uuid.UUID) ([]*types.Path, error)
	ListByStatus(ctx context.Context, tx *gorm.DB, statuses []string) ([]*types.Path, error)

	Update(ctx context.Context, tx *gorm.DB, row *types.Path) error
	UpdateFields(ctx context.Context, tx *gorm.DB, id uuid.UUID, updates map[string]interface{}) error

	SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
	SoftDeleteByUserIDs(ctx context.Context, tx *gorm.DB, userIDs []uuid.UUID) error
	FullDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
	FullDeleteByUserIDs(ctx context.Context, tx *gorm.DB, userIDs []uuid.UUID) error
}

type pathRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewPathRepo(db *gorm.DB, baseLog *logger.Logger) PathRepo {
	return &pathRepo{db: db, log: baseLog.With("repo", "PathRepo")}
}

func (r *pathRepo) Create(ctx context.Context, tx *gorm.DB, rows []*types.Path) ([]*types.Path, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return []*types.Path{}, nil
	}
	if err := t.WithContext(ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *pathRepo) GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.Path, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.Path
	if len(ids) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).Where("id IN ?", ids).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *pathRepo) GetByID(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*types.Path, error) {
	if id == uuid.Nil {
		return nil, nil
	}
	rows, err := r.GetByIDs(ctx, tx, []uuid.UUID{id})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

func (r *pathRepo) ListByUser(ctx context.Context, tx *gorm.DB, userID *uuid.UUID) ([]*types.Path, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.Path

	cleanUserID := userID
	if cleanUserID != nil && *cleanUserID == uuid.Nil {
		cleanUserID = nil
	}

	if err := t.WithContext(ctx).
		Where("user_id IS NOT DISTINCT FROM ?", cleanUserID).
		Order("created_at DESC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *pathRepo) ListByUserIDs(ctx context.Context, tx *gorm.DB, userIDs []uuid.UUID) ([]*types.Path, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.Path
	if len(userIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("user_id IN ?", userIDs).
		Order("user_id ASC, created_at DESC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *pathRepo) ListByStatus(ctx context.Context, tx *gorm.DB, statuses []string) ([]*types.Path, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.Path
	if len(statuses) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("status IN ?", statuses).
		Order("created_at DESC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *pathRepo) Update(ctx context.Context, tx *gorm.DB, row *types.Path) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if row == nil {
		return nil
	}
	return t.WithContext(ctx).Save(row).Error
}

func (r *pathRepo) UpdateFields(ctx context.Context, tx *gorm.DB, id uuid.UUID, updates map[string]interface{}) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if id == uuid.Nil {
		return nil
	}
	if updates == nil {
		updates = map[string]interface{}{}
	}
	if _, ok := updates["updated_at"]; !ok {
		updates["updated_at"] = time.Now().UTC()
	}
	return t.WithContext(ctx).
		Model(&types.Path{}).
		Where("id = ?", id).
		Updates(updates).Error
}

func (r *pathRepo) SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(ctx).Where("id IN ?", ids).Delete(&types.Path{}).Error
}

func (r *pathRepo) SoftDeleteByUserIDs(ctx context.Context, tx *gorm.DB, userIDs []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(userIDs) == 0 {
		return nil
	}
	return t.WithContext(ctx).Where("user_id IN ?", userIDs).Delete(&types.Path{}).Error
}

func (r *pathRepo) FullDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(ctx).Unscoped().Where("id IN ?", ids).Delete(&types.Path{}).Error
}

func (r *pathRepo) FullDeleteByUserIDs(ctx context.Context, tx *gorm.DB, userIDs []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(userIDs) == 0 {
		return nil
	}
	return t.WithContext(ctx).Unscoped().Where("user_id IN ?", userIDs).Delete(&types.Path{}).Error
}
