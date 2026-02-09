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

type DocGenerationTraceRepo interface {
	GetByID(dbc dbctx.Context, id uuid.UUID) (*types.DocGenerationTrace, error)
	GetByTraceID(dbc dbctx.Context, traceID string) (*types.DocGenerationTrace, error)
	ListByUserAndNode(dbc dbctx.Context, userID, pathNodeID uuid.UUID, limit int) ([]*types.DocGenerationTrace, error)
	Upsert(dbc dbctx.Context, row *types.DocGenerationTrace) error
}

type docGenerationTraceRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewDocGenerationTraceRepo(db *gorm.DB, baseLog *logger.Logger) DocGenerationTraceRepo {
	return &docGenerationTraceRepo{db: db, log: baseLog.With("repo", "DocGenerationTraceRepo")}
}

func (r *docGenerationTraceRepo) GetByID(dbc dbctx.Context, id uuid.UUID) (*types.DocGenerationTrace, error) {
	if id == uuid.Nil {
		return nil, nil
	}
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out types.DocGenerationTrace
	if err := t.WithContext(dbc.Ctx).First(&out, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &out, nil
}

func (r *docGenerationTraceRepo) GetByTraceID(dbc dbctx.Context, traceID string) (*types.DocGenerationTrace, error) {
	if traceID == "" {
		return nil, nil
	}
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out types.DocGenerationTrace
	if err := t.WithContext(dbc.Ctx).First(&out, "trace_id = ?", traceID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &out, nil
}

func (r *docGenerationTraceRepo) ListByUserAndNode(dbc dbctx.Context, userID, pathNodeID uuid.UUID, limit int) ([]*types.DocGenerationTrace, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.DocGenerationTrace
	if userID == uuid.Nil || pathNodeID == uuid.Nil {
		return out, nil
	}
	q := t.WithContext(dbc.Ctx).
		Where("user_id = ? AND path_node_id = ?", userID, pathNodeID).
		Order("created_at DESC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	if err := q.Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *docGenerationTraceRepo) Upsert(dbc dbctx.Context, row *types.DocGenerationTrace) error {
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
	if row.CreatedAt.IsZero() {
		row.CreatedAt = time.Now().UTC()
	}

	return t.WithContext(dbc.Ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "trace_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"user_id",
				"path_id",
				"path_node_id",
				"policy_version",
				"schema_version",
				"model",
				"prompt_hash",
				"retrieval_pack_id",
				"blueprint_version",
				"trace_json",
			}),
		}).
		Create(row).Error
}
