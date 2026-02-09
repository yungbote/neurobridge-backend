package learning

import (
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type DocProbeRepo interface {
	Upsert(dbc dbctx.Context, row *types.DocProbe) error
	GetByUserNodeBlock(dbc dbctx.Context, userID, pathNodeID uuid.UUID, blockID string) (*types.DocProbe, error)
	ListByUserAndNode(dbc dbctx.Context, userID, pathNodeID uuid.UUID) ([]*types.DocProbe, error)
	ListActiveByUserAndNode(dbc dbctx.Context, userID, pathNodeID uuid.UUID) ([]*types.DocProbe, error)
	CountByUserSince(dbc dbctx.Context, userID uuid.UUID, since time.Time) (int64, error)
}

type docProbeRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewDocProbeRepo(db *gorm.DB, baseLog *logger.Logger) DocProbeRepo {
	return &docProbeRepo{db: db, log: baseLog.With("repo", "DocProbeRepo")}
}

func (r *docProbeRepo) Upsert(dbc dbctx.Context, row *types.DocProbe) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if row == nil || row.UserID == uuid.Nil || row.PathID == uuid.Nil || row.PathNodeID == uuid.Nil {
		return nil
	}
	row.BlockID = strings.TrimSpace(row.BlockID)
	if row.BlockID == "" {
		return nil
	}
	row.BlockType = strings.TrimSpace(row.BlockType)
	row.ProbeKind = strings.TrimSpace(row.ProbeKind)
	if row.ProbeKind == "" {
		row.ProbeKind = row.BlockType
	}

	now := time.Now().UTC()
	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	if row.CreatedAt.IsZero() {
		row.CreatedAt = now
	}
	row.UpdatedAt = now

	return t.WithContext(dbc.Ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "user_id"}, {Name: "path_node_id"}, {Name: "block_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"path_id",
				"block_type",
				"probe_kind",
				"concept_keys",
				"concept_ids",
				"trigger_after_block_ids",
				"info_gain",
				"score",
				"policy_version",
				"schema_version",
				"status",
				"shown_count",
				"shown_at",
				"completed_at",
				"dismissed_at",
				"metadata",
				"updated_at",
			}),
		}).
		Create(row).Error
}

func (r *docProbeRepo) GetByUserNodeBlock(dbc dbctx.Context, userID, pathNodeID uuid.UUID, blockID string) (*types.DocProbe, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	blockID = strings.TrimSpace(blockID)
	if userID == uuid.Nil || pathNodeID == uuid.Nil || blockID == "" {
		return nil, nil
	}
	var out types.DocProbe
	if err := t.WithContext(dbc.Ctx).
		Where("user_id = ? AND path_node_id = ? AND block_id = ?", userID, pathNodeID, blockID).
		Limit(1).
		Find(&out).Error; err != nil {
		return nil, err
	}
	if out.ID == uuid.Nil {
		return nil, nil
	}
	return &out, nil
}

func (r *docProbeRepo) ListByUserAndNode(dbc dbctx.Context, userID, pathNodeID uuid.UUID) ([]*types.DocProbe, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	out := []*types.DocProbe{}
	if userID == uuid.Nil || pathNodeID == uuid.Nil {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("user_id = ? AND path_node_id = ?", userID, pathNodeID).
		Order("created_at DESC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *docProbeRepo) ListActiveByUserAndNode(dbc dbctx.Context, userID, pathNodeID uuid.UUID) ([]*types.DocProbe, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	out := []*types.DocProbe{}
	if userID == uuid.Nil || pathNodeID == uuid.Nil {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("user_id = ? AND path_node_id = ?", userID, pathNodeID).
		Where("status IN ?", []string{"planned", "shown"}).
		Order("created_at DESC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *docProbeRepo) CountByUserSince(dbc dbctx.Context, userID uuid.UUID, since time.Time) (int64, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if userID == uuid.Nil {
		return 0, nil
	}
	var count int64
	q := t.WithContext(dbc.Ctx).Model(&types.DocProbe{}).
		Where("user_id = ?", userID)
	if !since.IsZero() {
		q = q.Where("created_at >= ?", since)
	}
	if err := q.Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}
