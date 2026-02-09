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

type PrereqGateDecisionRepo interface {
	GetLatestByUserAndNode(dbc dbctx.Context, userID, pathNodeID uuid.UUID) (*types.PrereqGateDecision, error)
	Upsert(dbc dbctx.Context, row *types.PrereqGateDecision) error
}

type prereqGateDecisionRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewPrereqGateDecisionRepo(db *gorm.DB, baseLog *logger.Logger) PrereqGateDecisionRepo {
	return &prereqGateDecisionRepo{db: db, log: baseLog.With("repo", "PrereqGateDecisionRepo")}
}

func (r *prereqGateDecisionRepo) GetLatestByUserAndNode(dbc dbctx.Context, userID, pathNodeID uuid.UUID) (*types.PrereqGateDecision, error) {
	if userID == uuid.Nil || pathNodeID == uuid.Nil {
		return nil, nil
	}
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out types.PrereqGateDecision
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

func (r *prereqGateDecisionRepo) Upsert(dbc dbctx.Context, row *types.PrereqGateDecision) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if row == nil || row.UserID == uuid.Nil || row.PathID == uuid.Nil || row.PathNodeID == uuid.Nil || row.SnapshotID == "" {
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
			Columns: []clause.Column{
				{Name: "user_id"},
				{Name: "path_node_id"},
				{Name: "snapshot_id"},
			},
			DoUpdates: clause.AssignmentColumns([]string{
				"path_id",
				"policy_version",
				"schema_version",
				"readiness_status",
				"readiness_score",
				"gate_mode",
				"decision",
				"reason",
				"evidence_json",
			}),
		}).
		Create(row).Error
}
