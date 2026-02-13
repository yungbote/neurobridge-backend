package products

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// Policy decision trace.
type DecisionTrace struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	UserID     uuid.UUID `gorm:"type:uuid;not null;index:idx_decision_time,priority:1" json:"user_id"`
	OccurredAt time.Time `gorm:"column:occurred_at;not null;index:idx_decision_time,priority:2" json:"occurred_at"`

	DecisionType  string     `gorm:"column:decision_type;not null;index" json:"decision_type"`
	DecisionPhase string     `gorm:"column:decision_phase;type:text;index" json:"decision_phase,omitempty"`
	DecisionMode  string     `gorm:"column:decision_mode;type:text;index" json:"decision_mode,omitempty"`
	PathID        *uuid.UUID `gorm:"type:uuid;index" json:"path_id,omitempty"`
	ActivityID    *uuid.UUID `gorm:"type:uuid;index" json:"activity_id,omitempty"`
	VariantID     *uuid.UUID `gorm:"type:uuid;index" json:"variant_id,omitempty"`

	RandomSeed      *string `gorm:"column:random_seed;type:text;index" json:"random_seed,omitempty"`
	ExplorationProb float64 `gorm:"column:exploration_prob;not null;default:0" json:"exploration_prob"`

	GraphVersion       string `gorm:"column:graph_version;type:text;index" json:"graph_version,omitempty"`
	EmbeddingVersion   string `gorm:"column:embedding_version;type:text;index" json:"embedding_version,omitempty"`
	TaxonomyVersion    string `gorm:"column:taxonomy_version;type:text;index" json:"taxonomy_version,omitempty"`
	ClusteringVersion  string `gorm:"column:clustering_version;type:text;index" json:"clustering_version,omitempty"`
	CalibrationVersion string `gorm:"column:calibration_version;type:text;index" json:"calibration_version,omitempty"`

	InterventionPlanID       string `gorm:"column:intervention_plan_id;type:text;index" json:"intervention_plan_id,omitempty"`
	InterventionPlanActionID string `gorm:"column:intervention_plan_action_id;type:text;index" json:"intervention_plan_action_id,omitempty"`

	Inputs     datatypes.JSON `gorm:"column:inputs;type:jsonb" json:"inputs"`
	Candidates datatypes.JSON `gorm:"column:candidates;type:jsonb" json:"candidates"`
	Chosen     datatypes.JSON `gorm:"column:chosen;type:jsonb" json:"chosen"`

	CreatedAt time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (DecisionTrace) TableName() string { return "decision_trace" }
