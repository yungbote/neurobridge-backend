package learning

import (
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type ModelSnapshotRepo interface {
	Create(dbc dbctx.Context, row *types.ModelSnapshot) error
	GetLatestByKey(dbc dbctx.Context, key string) (*types.ModelSnapshot, error)
	ListByKey(dbc dbctx.Context, key string, limit int) ([]*types.ModelSnapshot, error)
	SetActiveByID(dbc dbctx.Context, id uuid.UUID) error
	Upsert(dbc dbctx.Context, row *types.ModelSnapshot) error
}

type modelSnapshotRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewModelSnapshotRepo(db *gorm.DB, baseLog *logger.Logger) ModelSnapshotRepo {
	return &modelSnapshotRepo{db: db, log: baseLog.With("repo", "ModelSnapshotRepo")}
}

func (r *modelSnapshotRepo) Create(dbc dbctx.Context, row *types.ModelSnapshot) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if row == nil || strings.TrimSpace(row.ModelKey) == "" {
		return nil
	}
	return t.WithContext(dbc.Ctx).Create(row).Error
}

func (r *modelSnapshotRepo) Upsert(dbc dbctx.Context, row *types.ModelSnapshot) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if row == nil || strings.TrimSpace(row.ModelKey) == "" {
		return nil
	}
	return t.WithContext(dbc.Ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "model_key"}, {Name: "version"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"active",
				"params_json",
				"metrics_json",
				"updated_at",
			}),
		}).
		Create(row).Error
}

func (r *modelSnapshotRepo) GetLatestByKey(dbc dbctx.Context, key string) (*types.ModelSnapshot, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, nil
	}
	row := &types.ModelSnapshot{}
	if err := t.WithContext(dbc.Ctx).
		Where("model_key = ?", key).
		Order("version DESC, created_at DESC").
		Limit(1).
		First(row).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return row, nil
}

func (r *modelSnapshotRepo) ListByKey(dbc dbctx.Context, key string, limit int) ([]*types.ModelSnapshot, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	key = strings.TrimSpace(key)
	out := []*types.ModelSnapshot{}
	if key == "" {
		return out, nil
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	if err := t.WithContext(dbc.Ctx).
		Where("model_key = ?", key).
		Order("version DESC, created_at DESC").
		Limit(limit).
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *modelSnapshotRepo) SetActiveByID(dbc dbctx.Context, id uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if id == uuid.Nil {
		return nil
	}
	var row types.ModelSnapshot
	if err := t.WithContext(dbc.Ctx).Where("id = ?", id).First(&row).Error; err != nil {
		return err
	}
	if err := t.WithContext(dbc.Ctx).
		Model(&types.ModelSnapshot{}).
		Where("model_key = ?", row.ModelKey).
		Update("active", false).Error; err != nil {
		return err
	}
	return t.WithContext(dbc.Ctx).
		Model(&types.ModelSnapshot{}).
		Where("id = ?", id).
		Update("active", true).Error
}
