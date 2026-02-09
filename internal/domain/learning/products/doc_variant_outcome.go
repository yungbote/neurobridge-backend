package products

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

// DocVariantOutcome stores evaluation outcomes for a variant exposure.
type DocVariantOutcome struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	ExposureID *uuid.UUID `gorm:"type:uuid;column:exposure_id;uniqueIndex:idx_doc_variant_outcome_exposure,priority:1;index" json:"exposure_id,omitempty"`

	UserID     uuid.UUID `gorm:"type:uuid;not null;index" json:"user_id"`
	PathID     uuid.UUID `gorm:"type:uuid;not null;index" json:"path_id"`
	PathNodeID uuid.UUID `gorm:"type:uuid;not null;index" json:"path_node_id"`

	VariantID *uuid.UUID `gorm:"type:uuid;column:variant_id;index" json:"variant_id,omitempty"`

	PolicyVersion string `gorm:"column:policy_version;type:text;not null;default:'base';index" json:"policy_version"`
	SchemaVersion int    `gorm:"column:schema_version;not null;default:1" json:"schema_version"`

	OutcomeKind string         `gorm:"column:outcome_kind;type:text;not null;default:'eval_v1';index" json:"outcome_kind"`
	MetricsJSON datatypes.JSON `gorm:"type:jsonb;column:metrics_json" json:"metrics_json,omitempty"`

	CreatedAt time.Time `gorm:"not null;default:now();index" json:"created_at"`
}

func (DocVariantOutcome) TableName() string { return "doc_variant_outcome" }
