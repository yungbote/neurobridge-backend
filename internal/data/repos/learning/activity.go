package learning

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type ActivityRepo interface {
	Create(ctx context.Context, tx *gorm.DB, rows []*types.Activity) ([]*types.Activity, error)

	GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.Activity, error)
	GetByID(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*types.Activity, error)

	ListByOwner(ctx context.Context, tx *gorm.DB, ownerType string, ownerID *uuid.UUID) ([]*types.Activity, error)
	ListByOwnerIDs(ctx context.Context, tx *gorm.DB, ownerType string, ownerIDs []uuid.UUID) ([]*types.Activity, error)
	ListByStatus(ctx context.Context, tx *gorm.DB, statuses []string) ([]*types.Activity, error)

	Update(ctx context.Context, tx *gorm.DB, row *types.Activity) error
	UpdateFields(ctx context.Context, tx *gorm.DB, id uuid.UUID, updates map[string]interface{}) error

	SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
	SoftDeleteByOwner(ctx context.Context, tx *gorm.DB, ownerType string, ownerID *uuid.UUID) error
	FullDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
	FullDeleteByOwner(ctx context.Context, tx *gorm.DB, ownerType string, ownerID *uuid.UUID) error
}

type activityRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewActivityRepo(db *gorm.DB, baseLog *logger.Logger) ActivityRepo {
	return &activityRepo{db: db, log: baseLog.With("repo", "ActivityRepo")}
}

func (r *activityRepo) Create(ctx context.Context, tx *gorm.DB, rows []*types.Activity) ([]*types.Activity, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return []*types.Activity{}, nil
	}
	if err := t.WithContext(ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *activityRepo) GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.Activity, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.Activity
	if len(ids) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("id IN ?", ids).
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *activityRepo) GetByID(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*types.Activity, error) {
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

func (r *activityRepo) ListByOwner(ctx context.Context, tx *gorm.DB, ownerType string, ownerID *uuid.UUID) ([]*types.Activity, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.Activity
	if ownerType == "" {
		return out, nil
	}

	cleanOwnerID := ownerID
	if cleanOwnerID != nil && *cleanOwnerID == uuid.Nil {
		cleanOwnerID = nil
	}

	if err := t.WithContext(ctx).
		Where("owner_type = ? AND owner_id IS NOT DISTINCT FROM ?", ownerType, cleanOwnerID).
		Order("created_at ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *activityRepo) ListByOwnerIDs(ctx context.Context, tx *gorm.DB, ownerType string, ownerIDs []uuid.UUID) ([]*types.Activity, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.Activity
	if ownerType == "" || len(ownerIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("owner_type = ? AND owner_id IN ?", ownerType, ownerIDs).
		Order("owner_id ASC, created_at ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *activityRepo) ListByStatus(ctx context.Context, tx *gorm.DB, statuses []string) ([]*types.Activity, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.Activity
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

func (r *activityRepo) Update(ctx context.Context, tx *gorm.DB, row *types.Activity) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if row == nil {
		return nil
	}
	return t.WithContext(ctx).Save(row).Error
}

func (r *activityRepo) UpdateFields(ctx context.Context, tx *gorm.DB, id uuid.UUID, updates map[string]interface{}) error {
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
		Model(&types.Activity{}).
		Where("id = ?", id).
		Updates(updates).Error
}

func (r *activityRepo) SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(ctx).Where("id IN ?", ids).Delete(&types.Activity{}).Error
}

func (r *activityRepo) SoftDeleteByOwner(ctx context.Context, tx *gorm.DB, ownerType string, ownerID *uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if ownerType == "" {
		return nil
	}

	cleanOwnerID := ownerID
	if cleanOwnerID != nil && *cleanOwnerID == uuid.Nil {
		cleanOwnerID = nil
	}

	return t.WithContext(ctx).
		Where("owner_type = ? AND owner_id IS NOT DISTINCT FROM ?", ownerType, cleanOwnerID).
		Delete(&types.Activity{}).Error
}

func (r *activityRepo) FullDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(ctx).Unscoped().Where("id IN ?", ids).Delete(&types.Activity{}).Error
}

func (r *activityRepo) FullDeleteByOwner(ctx context.Context, tx *gorm.DB, ownerType string, ownerID *uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if ownerType == "" {
		return nil
	}

	cleanOwnerID := ownerID
	if cleanOwnerID != nil && *cleanOwnerID == uuid.Nil {
		cleanOwnerID = nil
	}

	return t.WithContext(ctx).
		Unscoped().
		Where("owner_type = ? AND owner_id IS NOT DISTINCT FROM ?", ownerType, cleanOwnerID).
		Delete(&types.Activity{}).Error
}
