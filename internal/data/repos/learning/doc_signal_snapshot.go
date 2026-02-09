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

type UserDocSignalSnapshotRepo interface {
	GetByID(dbc dbctx.Context, id uuid.UUID) (*types.UserDocSignalSnapshot, error)
	GetBySnapshotID(dbc dbctx.Context, snapshotID string) (*types.UserDocSignalSnapshot, error)
	GetLatestByUserAndNode(dbc dbctx.Context, userID, pathNodeID uuid.UUID) (*types.UserDocSignalSnapshot, error)
	Upsert(dbc dbctx.Context, row *types.UserDocSignalSnapshot) error
}

type userDocSignalSnapshotRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewUserDocSignalSnapshotRepo(db *gorm.DB, baseLog *logger.Logger) UserDocSignalSnapshotRepo {
	return &userDocSignalSnapshotRepo{db: db, log: baseLog.With("repo", "UserDocSignalSnapshotRepo")}
}

func (r *userDocSignalSnapshotRepo) GetByID(dbc dbctx.Context, id uuid.UUID) (*types.UserDocSignalSnapshot, error) {
	if id == uuid.Nil {
		return nil, nil
	}
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out types.UserDocSignalSnapshot
	if err := t.WithContext(dbc.Ctx).First(&out, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &out, nil
}

func (r *userDocSignalSnapshotRepo) GetBySnapshotID(dbc dbctx.Context, snapshotID string) (*types.UserDocSignalSnapshot, error) {
	if snapshotID == "" {
		return nil, nil
	}
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out types.UserDocSignalSnapshot
	if err := t.WithContext(dbc.Ctx).First(&out, "snapshot_id = ?", snapshotID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &out, nil
}

func (r *userDocSignalSnapshotRepo) GetLatestByUserAndNode(dbc dbctx.Context, userID, pathNodeID uuid.UUID) (*types.UserDocSignalSnapshot, error) {
	if userID == uuid.Nil || pathNodeID == uuid.Nil {
		return nil, nil
	}
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out types.UserDocSignalSnapshot
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

func (r *userDocSignalSnapshotRepo) Upsert(dbc dbctx.Context, row *types.UserDocSignalSnapshot) error {
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
				"snapshot_json",
			}),
		}).
		Create(row).Error
}
