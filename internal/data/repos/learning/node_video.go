package learning

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
)

type LearningNodeVideoRepo interface {
	GetByID(dbc dbctx.Context, id uuid.UUID) (*types.LearningNodeVideo, error)
	GetByPathNodeIDs(dbc dbctx.Context, pathNodeIDs []uuid.UUID) ([]*types.LearningNodeVideo, error)

	Upsert(dbc dbctx.Context, row *types.LearningNodeVideo) error
}

type learningNodeVideoRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewLearningNodeVideoRepo(db *gorm.DB, baseLog *logger.Logger) LearningNodeVideoRepo {
	return &learningNodeVideoRepo{db: db, log: baseLog.With("repo", "LearningNodeVideoRepo")}
}

func (r *learningNodeVideoRepo) GetByID(dbc dbctx.Context, id uuid.UUID) (*types.LearningNodeVideo, error) {
	if id == uuid.Nil {
		return nil, nil
	}
	rows, err := r.getByIDs(dbc, []uuid.UUID{id})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

func (r *learningNodeVideoRepo) getByIDs(dbc dbctx.Context, ids []uuid.UUID) ([]*types.LearningNodeVideo, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.LearningNodeVideo
	if len(ids) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).Where("id IN ?", ids).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *learningNodeVideoRepo) GetByPathNodeIDs(dbc dbctx.Context, pathNodeIDs []uuid.UUID) ([]*types.LearningNodeVideo, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.LearningNodeVideo
	if len(pathNodeIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("path_node_id IN ?", pathNodeIDs).
		Order("path_node_id ASC, slot ASC, updated_at DESC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *learningNodeVideoRepo) Upsert(dbc dbctx.Context, row *types.LearningNodeVideo) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if row == nil || row.UserID == uuid.Nil || row.PathID == uuid.Nil || row.PathNodeID == uuid.Nil {
		return nil
	}
	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	now := time.Now().UTC()
	row.UpdatedAt = now
	if row.CreatedAt.IsZero() {
		row.CreatedAt = now
	}

	return t.WithContext(dbc.Ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "user_id"}, {Name: "path_node_id"}, {Name: "slot"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"path_id",
				"schema_version",
				"plan_json",
				"prompt_hash",
				"sources_hash",
				"status",
				"asset_id",
				"asset_storage_key",
				"asset_url",
				"asset_mime_type",
				"error",
				"updated_at",
			}),
		}).
		Create(row).Error
}
