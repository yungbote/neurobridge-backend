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

type ConceptReadinessSnapshotRepo interface {
	GetBySnapshotID(dbc dbctx.Context, snapshotID string) (*types.ConceptReadinessSnapshot, error)
	GetLatestByUserAndNode(dbc dbctx.Context, userID, pathNodeID uuid.UUID) (*types.ConceptReadinessSnapshot, error)
	Upsert(dbc dbctx.Context, row *types.ConceptReadinessSnapshot) error
}

type conceptReadinessSnapshotRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewConceptReadinessSnapshotRepo(db *gorm.DB, baseLog *logger.Logger) ConceptReadinessSnapshotRepo {
	return &conceptReadinessSnapshotRepo{db: db, log: baseLog.With("repo", "ConceptReadinessSnapshotRepo")}
}

func (r *conceptReadinessSnapshotRepo) GetBySnapshotID(dbc dbctx.Context, snapshotID string) (*types.ConceptReadinessSnapshot, error) {
	if snapshotID == "" {
		return nil, nil
	}
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out types.ConceptReadinessSnapshot
	if err := t.WithContext(dbc.Ctx).First(&out, "snapshot_id = ?", snapshotID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &out, nil
}

func (r *conceptReadinessSnapshotRepo) GetLatestByUserAndNode(dbc dbctx.Context, userID, pathNodeID uuid.UUID) (*types.ConceptReadinessSnapshot, error) {
	if userID == uuid.Nil || pathNodeID == uuid.Nil {
		return nil, nil
	}
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out types.ConceptReadinessSnapshot
	if err := t.WithContext(dbc.Ctx).
		Where("user_id = ? AND path_node_id = ?", userID, pathNodeID).
		Order("created_at DESC").
		Limit(1).
		Find(&out).Error; err != nil {
		return nil, err
	}
	if out.ID == uuid.Nil {
		return nil, nil
	}
	return &out, nil
}

func (r *conceptReadinessSnapshotRepo) Upsert(dbc dbctx.Context, row *types.ConceptReadinessSnapshot) error {
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
			Columns: []clause.Column{{Name: "snapshot_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"user_id",
				"path_id",
				"path_node_id",
				"policy_version",
				"schema_version",
				"status",
				"score",
				"snapshot_json",
			}),
		}).
		Create(row).Error
}
