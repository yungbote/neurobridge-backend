package learning

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type LearningNodeDocRepo interface {
	GetByID(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*types.LearningNodeDoc, error)
	GetByPathNodeID(ctx context.Context, tx *gorm.DB, pathNodeID uuid.UUID) (*types.LearningNodeDoc, error)
	GetByPathNodeIDs(ctx context.Context, tx *gorm.DB, pathNodeIDs []uuid.UUID) ([]*types.LearningNodeDoc, error)

	Upsert(ctx context.Context, tx *gorm.DB, row *types.LearningNodeDoc) error
}

type learningNodeDocRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewLearningNodeDocRepo(db *gorm.DB, baseLog *logger.Logger) LearningNodeDocRepo {
	return &learningNodeDocRepo{db: db, log: baseLog.With("repo", "LearningNodeDocRepo")}
}

func (r *learningNodeDocRepo) GetByID(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*types.LearningNodeDoc, error) {
	if id == uuid.Nil {
		return nil, nil
	}
	rows, err := r.getByIDs(ctx, tx, []uuid.UUID{id})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

func (r *learningNodeDocRepo) getByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.LearningNodeDoc, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.LearningNodeDoc
	if len(ids) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).Where("id IN ?", ids).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *learningNodeDocRepo) GetByPathNodeID(ctx context.Context, tx *gorm.DB, pathNodeID uuid.UUID) (*types.LearningNodeDoc, error) {
	if pathNodeID == uuid.Nil {
		return nil, nil
	}
	rows, err := r.GetByPathNodeIDs(ctx, tx, []uuid.UUID{pathNodeID})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

func (r *learningNodeDocRepo) GetByPathNodeIDs(ctx context.Context, tx *gorm.DB, pathNodeIDs []uuid.UUID) ([]*types.LearningNodeDoc, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.LearningNodeDoc
	if len(pathNodeIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("path_node_id IN ?", pathNodeIDs).
		Order("updated_at DESC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *learningNodeDocRepo) Upsert(ctx context.Context, tx *gorm.DB, row *types.LearningNodeDoc) error {
	t := tx
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

	return t.WithContext(ctx).
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
