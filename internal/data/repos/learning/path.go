package learning

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type PathRepo interface {
	Create(dbc dbctx.Context, rows []*types.Path) ([]*types.Path, error)

	GetByIDs(dbc dbctx.Context, ids []uuid.UUID) ([]*types.Path, error)
	GetByID(dbc dbctx.Context, id uuid.UUID) (*types.Path, error)

	ListByUser(dbc dbctx.Context, userID *uuid.UUID) ([]*types.Path, error)
	ListByUserIDs(dbc dbctx.Context, userIDs []uuid.UUID) ([]*types.Path, error)
	ListByStatus(dbc dbctx.Context, statuses []string) ([]*types.Path, error)

	Update(dbc dbctx.Context, row *types.Path) error
	UpdateFields(dbc dbctx.Context, id uuid.UUID, updates map[string]interface{}) error

	SoftDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error
	SoftDeleteByUserIDs(dbc dbctx.Context, userIDs []uuid.UUID) error
	FullDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error
	FullDeleteByUserIDs(dbc dbctx.Context, userIDs []uuid.UUID) error
}

type pathRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewPathRepo(db *gorm.DB, baseLog *logger.Logger) PathRepo {
	return &pathRepo{db: db, log: baseLog.With("repo", "PathRepo")}
}

func (r *pathRepo) Create(dbc dbctx.Context, rows []*types.Path) ([]*types.Path, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return []*types.Path{}, nil
	}
	if err := t.WithContext(dbc.Ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *pathRepo) GetByIDs(dbc dbctx.Context, ids []uuid.UUID) ([]*types.Path, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.Path
	if len(ids) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).Where("id IN ?", ids).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *pathRepo) GetByID(dbc dbctx.Context, id uuid.UUID) (*types.Path, error) {
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

func (r *pathRepo) ListByUser(dbc dbctx.Context, userID *uuid.UUID) ([]*types.Path, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.Path

	cleanUserID := userID
	if cleanUserID != nil && *cleanUserID == uuid.Nil {
		cleanUserID = nil
	}

	if err := t.WithContext(dbc.Ctx).
		Where("user_id IS NOT DISTINCT FROM ?", cleanUserID).
		Order("created_at DESC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *pathRepo) ListByUserIDs(dbc dbctx.Context, userIDs []uuid.UUID) ([]*types.Path, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.Path
	if len(userIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("user_id IN ?", userIDs).
		Order("user_id ASC, created_at DESC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *pathRepo) ListByStatus(dbc dbctx.Context, statuses []string) ([]*types.Path, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.Path
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

func (r *pathRepo) Update(dbc dbctx.Context, row *types.Path) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if row == nil {
		return nil
	}
	return t.WithContext(dbc.Ctx).Save(row).Error
}

func (r *pathRepo) UpdateFields(dbc dbctx.Context, id uuid.UUID, updates map[string]interface{}) error {
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
		Model(&types.Path{}).
		Where("id = ?", id).
		Updates(updates).Error
}

func (r *pathRepo) SoftDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Where("id IN ?", ids).Delete(&types.Path{}).Error
}

func (r *pathRepo) SoftDeleteByUserIDs(dbc dbctx.Context, userIDs []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(userIDs) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Where("user_id IN ?", userIDs).Delete(&types.Path{}).Error
}

func (r *pathRepo) FullDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Unscoped().Where("id IN ?", ids).Delete(&types.Path{}).Error
}

func (r *pathRepo) FullDeleteByUserIDs(dbc dbctx.Context, userIDs []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(userIDs) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Unscoped().Where("user_id IN ?", userIDs).Delete(&types.Path{}).Error
}
