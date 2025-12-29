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

type LearningNodeFigureRepo interface {
	GetByID(dbc dbctx.Context, id uuid.UUID) (*types.LearningNodeFigure, error)
	GetByPathNodeIDs(dbc dbctx.Context, pathNodeIDs []uuid.UUID) ([]*types.LearningNodeFigure, error)

	Upsert(dbc dbctx.Context, row *types.LearningNodeFigure) error
}

type learningNodeFigureRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewLearningNodeFigureRepo(db *gorm.DB, baseLog *logger.Logger) LearningNodeFigureRepo {
	return &learningNodeFigureRepo{db: db, log: baseLog.With("repo", "LearningNodeFigureRepo")}
}

func (r *learningNodeFigureRepo) GetByID(dbc dbctx.Context, id uuid.UUID) (*types.LearningNodeFigure, error) {
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

func (r *learningNodeFigureRepo) getByIDs(dbc dbctx.Context, ids []uuid.UUID) ([]*types.LearningNodeFigure, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.LearningNodeFigure
	if len(ids) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).Where("id IN ?", ids).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *learningNodeFigureRepo) GetByPathNodeIDs(dbc dbctx.Context, pathNodeIDs []uuid.UUID) ([]*types.LearningNodeFigure, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.LearningNodeFigure
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

func (r *learningNodeFigureRepo) Upsert(dbc dbctx.Context, row *types.LearningNodeFigure) error {
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

