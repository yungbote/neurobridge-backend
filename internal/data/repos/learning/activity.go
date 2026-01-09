package learning

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type ActivityRepo interface {
	Create(dbc dbctx.Context, rows []*types.Activity) ([]*types.Activity, error)

	GetByIDs(dbc dbctx.Context, ids []uuid.UUID) ([]*types.Activity, error)
	GetByID(dbc dbctx.Context, id uuid.UUID) (*types.Activity, error)

	ListByOwner(dbc dbctx.Context, ownerType string, ownerID *uuid.UUID) ([]*types.Activity, error)
	ListByOwnerIDs(dbc dbctx.Context, ownerType string, ownerIDs []uuid.UUID) ([]*types.Activity, error)
	ListByStatus(dbc dbctx.Context, statuses []string) ([]*types.Activity, error)

	Update(dbc dbctx.Context, row *types.Activity) error
	UpdateFields(dbc dbctx.Context, id uuid.UUID, updates map[string]interface{}) error

	SoftDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error
	SoftDeleteByOwner(dbc dbctx.Context, ownerType string, ownerID *uuid.UUID) error
	FullDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error
	FullDeleteByOwner(dbc dbctx.Context, ownerType string, ownerID *uuid.UUID) error
}

type activityRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewActivityRepo(db *gorm.DB, baseLog *logger.Logger) ActivityRepo {
	return &activityRepo{db: db, log: baseLog.With("repo", "ActivityRepo")}
}

func (r *activityRepo) Create(dbc dbctx.Context, rows []*types.Activity) ([]*types.Activity, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return []*types.Activity{}, nil
	}
	if err := t.WithContext(dbc.Ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *activityRepo) GetByIDs(dbc dbctx.Context, ids []uuid.UUID) ([]*types.Activity, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.Activity
	if len(ids) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("id IN ?", ids).
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *activityRepo) GetByID(dbc dbctx.Context, id uuid.UUID) (*types.Activity, error) {
	if id == uuid.Nil {
		return nil, nil
	}
	rows, err := r.GetByIDs(dbc, []uuid.UUID{id})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

func (r *activityRepo) ListByOwner(dbc dbctx.Context, ownerType string, ownerID *uuid.UUID) ([]*types.Activity, error) {
	t := dbc.Tx
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

	if err := t.WithContext(dbc.Ctx).
		Where("owner_type = ? AND owner_id IS NOT DISTINCT FROM ?", ownerType, cleanOwnerID).
		Order("created_at ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *activityRepo) ListByOwnerIDs(dbc dbctx.Context, ownerType string, ownerIDs []uuid.UUID) ([]*types.Activity, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.Activity
	if ownerType == "" || len(ownerIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("owner_type = ? AND owner_id IN ?", ownerType, ownerIDs).
		Order("owner_id ASC, created_at ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *activityRepo) ListByStatus(dbc dbctx.Context, statuses []string) ([]*types.Activity, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.Activity
	if len(statuses) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("status IN ?", statuses).
		Order("created_at DESC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *activityRepo) Update(dbc dbctx.Context, row *types.Activity) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if row == nil {
		return nil
	}
	return t.WithContext(dbc.Ctx).Save(row).Error
}

func (r *activityRepo) UpdateFields(dbc dbctx.Context, id uuid.UUID, updates map[string]interface{}) error {
	t := dbc.Tx
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
	return t.WithContext(dbc.Ctx).
		Model(&types.Activity{}).
		Where("id = ?", id).
		Updates(updates).Error
}

func (r *activityRepo) SoftDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Where("id IN ?", ids).Delete(&types.Activity{}).Error
}

func (r *activityRepo) SoftDeleteByOwner(dbc dbctx.Context, ownerType string, ownerID *uuid.UUID) error {
	t := dbc.Tx
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

	return t.WithContext(dbc.Ctx).
		Where("owner_type = ? AND owner_id IS NOT DISTINCT FROM ?", ownerType, cleanOwnerID).
		Delete(&types.Activity{}).Error
}

func (r *activityRepo) FullDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Unscoped().Where("id IN ?", ids).Delete(&types.Activity{}).Error
}

func (r *activityRepo) FullDeleteByOwner(dbc dbctx.Context, ownerType string, ownerID *uuid.UUID) error {
	t := dbc.Tx
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

	return t.WithContext(dbc.Ctx).
		Unscoped().
		Where("owner_type = ? AND owner_id IS NOT DISTINCT FROM ?", ownerType, cleanOwnerID).
		Delete(&types.Activity{}).Error
}
