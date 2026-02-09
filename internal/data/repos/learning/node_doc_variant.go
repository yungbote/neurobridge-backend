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

type LearningNodeDocVariantRepo interface {
	GetByID(dbc dbctx.Context, id uuid.UUID) (*types.LearningNodeDocVariant, error)
	GetBySnapshotID(dbc dbctx.Context, snapshotID string) (*types.LearningNodeDocVariant, error)
	GetLatestByUserAndNode(dbc dbctx.Context, userID, pathNodeID uuid.UUID) (*types.LearningNodeDocVariant, error)
	ListByUserAndNode(dbc dbctx.Context, userID, pathNodeID uuid.UUID, limit int) ([]*types.LearningNodeDocVariant, error)
	Upsert(dbc dbctx.Context, row *types.LearningNodeDocVariant) error
}

type learningNodeDocVariantRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewLearningNodeDocVariantRepo(db *gorm.DB, baseLog *logger.Logger) LearningNodeDocVariantRepo {
	return &learningNodeDocVariantRepo{db: db, log: baseLog.With("repo", "LearningNodeDocVariantRepo")}
}

func (r *learningNodeDocVariantRepo) GetByID(dbc dbctx.Context, id uuid.UUID) (*types.LearningNodeDocVariant, error) {
	if id == uuid.Nil {
		return nil, nil
	}
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out types.LearningNodeDocVariant
	if err := t.WithContext(dbc.Ctx).First(&out, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &out, nil
}

func (r *learningNodeDocVariantRepo) GetBySnapshotID(dbc dbctx.Context, snapshotID string) (*types.LearningNodeDocVariant, error) {
	if snapshotID == "" {
		return nil, nil
	}
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out types.LearningNodeDocVariant
	if err := t.WithContext(dbc.Ctx).First(&out, "snapshot_id = ?", snapshotID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &out, nil
}

func (r *learningNodeDocVariantRepo) GetLatestByUserAndNode(dbc dbctx.Context, userID, pathNodeID uuid.UUID) (*types.LearningNodeDocVariant, error) {
	if userID == uuid.Nil || pathNodeID == uuid.Nil {
		return nil, nil
	}
	rows, err := r.ListByUserAndNode(dbc, userID, pathNodeID, 1)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

func (r *learningNodeDocVariantRepo) ListByUserAndNode(dbc dbctx.Context, userID, pathNodeID uuid.UUID, limit int) ([]*types.LearningNodeDocVariant, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.LearningNodeDocVariant
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

func (r *learningNodeDocVariantRepo) Upsert(dbc dbctx.Context, row *types.LearningNodeDocVariant) error {
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
			Columns: []clause.Column{{Name: "snapshot_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"user_id",
				"path_id",
				"path_node_id",
				"base_doc_id",
				"variant_kind",
				"policy_version",
				"schema_version",
				"retrieval_pack_id",
				"trace_id",
				"doc_json",
				"doc_text",
				"content_hash",
				"sources_hash",
				"status",
				"expires_at",
				"updated_at",
			}),
		}).
		Create(row).Error
}
