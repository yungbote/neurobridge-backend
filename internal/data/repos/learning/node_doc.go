package learning

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type LearningNodeDocRepo interface {
	GetByID(dbc dbctx.Context, id uuid.UUID) (*types.LearningNodeDoc, error)
	GetByPathNodeID(dbc dbctx.Context, pathNodeID uuid.UUID) (*types.LearningNodeDoc, error)
	GetByPathNodeIDs(dbc dbctx.Context, pathNodeIDs []uuid.UUID) ([]*types.LearningNodeDoc, error)

	Upsert(dbc dbctx.Context, row *types.LearningNodeDoc) error
}

type learningNodeDocRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewLearningNodeDocRepo(db *gorm.DB, baseLog *logger.Logger) LearningNodeDocRepo {
	return &learningNodeDocRepo{db: db, log: baseLog.With("repo", "LearningNodeDocRepo")}
}

func (r *learningNodeDocRepo) GetByID(dbc dbctx.Context, id uuid.UUID) (*types.LearningNodeDoc, error) {
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

func (r *learningNodeDocRepo) getByIDs(dbc dbctx.Context, ids []uuid.UUID) ([]*types.LearningNodeDoc, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.LearningNodeDoc
	if len(ids) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).Where("id IN ?", ids).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *learningNodeDocRepo) GetByPathNodeID(dbc dbctx.Context, pathNodeID uuid.UUID) (*types.LearningNodeDoc, error) {
	if pathNodeID == uuid.Nil {
		return nil, nil
	}
	rows, err := r.GetByPathNodeIDs(dbc, []uuid.UUID{pathNodeID})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

func (r *learningNodeDocRepo) GetByPathNodeIDs(dbc dbctx.Context, pathNodeIDs []uuid.UUID) ([]*types.LearningNodeDoc, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.LearningNodeDoc
	if len(pathNodeIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("path_node_id IN ?", pathNodeIDs).
		Order("updated_at DESC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *learningNodeDocRepo) Upsert(dbc dbctx.Context, row *types.LearningNodeDoc) error {
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
			Columns: []clause.Column{{Name: "path_node_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"user_id",
				"path_id",
				"schema_version",
				"doc_json",
				"doc_text",
				"content_hash",
				"sources_hash",
				"updated_at",
			}),
		}).
		Create(row).Error
}
