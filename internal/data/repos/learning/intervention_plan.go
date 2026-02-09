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

type InterventionPlanRepo interface {
	GetByPlanID(dbc dbctx.Context, planID string) (*types.InterventionPlan, error)
	GetLatestByUserAndNode(dbc dbctx.Context, userID, pathNodeID uuid.UUID) (*types.InterventionPlan, error)
	Upsert(dbc dbctx.Context, row *types.InterventionPlan) error
}

type interventionPlanRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewInterventionPlanRepo(db *gorm.DB, baseLog *logger.Logger) InterventionPlanRepo {
	return &interventionPlanRepo{db: db, log: baseLog.With("repo", "InterventionPlanRepo")}
}

func (r *interventionPlanRepo) GetByPlanID(dbc dbctx.Context, planID string) (*types.InterventionPlan, error) {
	if planID == "" {
		return nil, nil
	}
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out types.InterventionPlan
	if err := t.WithContext(dbc.Ctx).First(&out, "plan_id = ?", planID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &out, nil
}

func (r *interventionPlanRepo) GetLatestByUserAndNode(dbc dbctx.Context, userID, pathNodeID uuid.UUID) (*types.InterventionPlan, error) {
	if userID == uuid.Nil || pathNodeID == uuid.Nil {
		return nil, nil
	}
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out types.InterventionPlan
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

func (r *interventionPlanRepo) Upsert(dbc dbctx.Context, row *types.InterventionPlan) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if row == nil || row.UserID == uuid.Nil || row.PathID == uuid.Nil || row.PathNodeID == uuid.Nil || row.PlanID == "" {
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
			Columns: []clause.Column{{Name: "plan_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"user_id",
				"path_id",
				"path_node_id",
				"snapshot_id",
				"policy_version",
				"schema_version",
				"flow_budget_total",
				"flow_budget_remaining",
				"expected_gain",
				"flow_cost",
				"actions_json",
				"constraints_json",
				"plan_json",
			}),
		}).
		Create(row).Error
}
