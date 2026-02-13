package products

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// StructuralDecisionTrace captures Tier A structural decisions with version and validation context.
type StructuralDecisionTrace struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	DecisionType  string `gorm:"column:decision_type;type:text;not null;index" json:"decision_type"`
	DecisionPhase string `gorm:"column:decision_phase;type:text;not null;index" json:"decision_phase"`
	DecisionMode  string `gorm:"column:decision_mode;type:text;not null;index" json:"decision_mode"`

	OccurredAt time.Time `gorm:"column:occurred_at;not null;index" json:"occurred_at"`

	UserID        *uuid.UUID `gorm:"type:uuid;index" json:"user_id,omitempty"`
	PathID        *uuid.UUID `gorm:"type:uuid;index" json:"path_id,omitempty"`
	MaterialSetID *uuid.UUID `gorm:"type:uuid;index" json:"material_set_id,omitempty"`
	SagaID        *uuid.UUID `gorm:"type:uuid;index" json:"saga_id,omitempty"`

	GraphVersion       string `gorm:"column:graph_version;type:text;index" json:"graph_version,omitempty"`
	EmbeddingVersion   string `gorm:"column:embedding_version;type:text;index" json:"embedding_version,omitempty"`
	TaxonomyVersion    string `gorm:"column:taxonomy_version;type:text;index" json:"taxonomy_version,omitempty"`
	ClusteringVersion  string `gorm:"column:clustering_version;type:text;index" json:"clustering_version,omitempty"`
	CalibrationVersion string `gorm:"column:calibration_version;type:text;index" json:"calibration_version,omitempty"`

	Inputs     datatypes.JSON `gorm:"column:inputs;type:jsonb" json:"inputs,omitempty"`
	Candidates datatypes.JSON `gorm:"column:candidates;type:jsonb" json:"candidates,omitempty"`
	Chosen     datatypes.JSON `gorm:"column:chosen;type:jsonb" json:"chosen,omitempty"`
	Thresholds datatypes.JSON `gorm:"column:thresholds;type:jsonb" json:"thresholds,omitempty"`

	Invariants       datatypes.JSON `gorm:"column:invariants;type:jsonb" json:"invariants,omitempty"`
	ValidationStatus string         `gorm:"column:validation_status;type:text;not null;default:'unknown';index" json:"validation_status"`

	Metadata datatypes.JSON `gorm:"column:metadata;type:jsonb" json:"metadata,omitempty"`

	CreatedAt time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (StructuralDecisionTrace) TableName() string { return "structural_decision_trace" }
