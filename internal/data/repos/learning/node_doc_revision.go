package learning

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type LearningNodeDocRevisionRepo interface {
	Create(dbc dbctx.Context, rows []*types.LearningNodeDocRevision) ([]*types.LearningNodeDocRevision, error)
	GetByID(dbc dbctx.Context, id uuid.UUID) (*types.LearningNodeDocRevision, error)
	ListByPathNodeID(dbc dbctx.Context, pathNodeID uuid.UUID, limit int) ([]*types.LearningNodeDocRevision, error)
	ListByDocID(dbc dbctx.Context, docID uuid.UUID, limit int) ([]*types.LearningNodeDocRevision, error)
}

type learningNodeDocRevisionRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewLearningNodeDocRevisionRepo(db *gorm.DB, baseLog *logger.Logger) LearningNodeDocRevisionRepo {
	return &learningNodeDocRevisionRepo{db: db, log: baseLog.With("repo", "LearningNodeDocRevisionRepo")}
}

func (r *learningNodeDocRevisionRepo) Create(dbc dbctx.Context, rows []*types.LearningNodeDocRevision) ([]*types.LearningNodeDocRevision, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return []*types.LearningNodeDocRevision{}, nil
	}
	for _, row := range rows {
		if row != nil && row.ID == uuid.Nil {
			row.ID = uuid.New()
		}
	}
	if err := t.WithContext(dbc.Ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *learningNodeDocRevisionRepo) GetByID(dbc dbctx.Context, id uuid.UUID) (*types.LearningNodeDocRevision, error) {
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

func (r *learningNodeDocRevisionRepo) getByIDs(dbc dbctx.Context, ids []uuid.UUID) ([]*types.LearningNodeDocRevision, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.LearningNodeDocRevision
	if len(ids) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).Where("id IN ?", ids).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *learningNodeDocRevisionRepo) ListByPathNodeID(dbc dbctx.Context, pathNodeID uuid.UUID, limit int) ([]*types.LearningNodeDocRevision, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.LearningNodeDocRevision
	if pathNodeID == uuid.Nil {
		return out, nil
	}
	q := t.WithContext(dbc.Ctx).Where("path_node_id = ?", pathNodeID).Order("created_at DESC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	if err := q.Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *learningNodeDocRevisionRepo) ListByDocID(dbc dbctx.Context, docID uuid.UUID, limit int) ([]*types.LearningNodeDocRevision, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.LearningNodeDocRevision
	if docID == uuid.Nil {
		return out, nil
	}
	q := t.WithContext(dbc.Ctx).Where("doc_id = ?", docID).Order("created_at DESC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	if err := q.Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}
