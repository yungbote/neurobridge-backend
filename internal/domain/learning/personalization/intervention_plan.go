package personalization

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

// InterventionPlan stores a runtime intervention plan derived from belief state.
type InterventionPlan struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	UserID     uuid.UUID `gorm:"type:uuid;not null;index:idx_intervention_plan,priority:1" json:"user_id"`
	PathID     uuid.UUID `gorm:"type:uuid;not null;index:idx_intervention_plan,priority:2" json:"path_id"`
	PathNodeID uuid.UUID `gorm:"type:uuid;not null;index:idx_intervention_plan,priority:3" json:"path_node_id"`

	PlanID        string `gorm:"column:plan_id;type:text;not null;uniqueIndex" json:"plan_id"`
	SnapshotID    string `gorm:"column:snapshot_id;type:text;not null;index" json:"snapshot_id"`
	PolicyVersion string `gorm:"column:policy_version;type:text;not null;index" json:"policy_version"`
	SchemaVersion int    `gorm:"column:schema_version;not null" json:"schema_version"`

	FlowBudgetTotal     float64 `gorm:"column:flow_budget_total;not null;default:0" json:"flow_budget_total"`
	FlowBudgetRemaining float64 `gorm:"column:flow_budget_remaining;not null;default:0" json:"flow_budget_remaining"`
	ExpectedGain        float64 `gorm:"column:expected_gain;not null;default:0" json:"expected_gain"`
	FlowCost            float64 `gorm:"column:flow_cost;not null;default:0" json:"flow_cost"`

	ActionsJSON     datatypes.JSON `gorm:"type:jsonb;column:actions_json" json:"actions_json,omitempty"`
	ConstraintsJSON datatypes.JSON `gorm:"type:jsonb;column:constraints_json" json:"constraints_json,omitempty"`
	PlanJSON        datatypes.JSON `gorm:"type:jsonb;column:plan_json;not null" json:"plan_json"`

	CreatedAt time.Time `gorm:"not null;default:now();index" json:"created_at"`
}

func (InterventionPlan) TableName() string { return "intervention_plan" }
