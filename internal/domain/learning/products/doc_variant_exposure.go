package products

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

// DocVariantExposure records which doc variant (or base doc) was served.
type DocVariantExposure struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	UserID     uuid.UUID `gorm:"type:uuid;not null;index" json:"user_id"`
	PathID     uuid.UUID `gorm:"type:uuid;not null;index" json:"path_id"`
	PathNodeID uuid.UUID `gorm:"type:uuid;not null;index" json:"path_node_id"`

	VariantID *uuid.UUID `gorm:"type:uuid;column:variant_id;index" json:"variant_id,omitempty"`
	BaseDocID *uuid.UUID `gorm:"type:uuid;column:base_doc_id;index" json:"base_doc_id,omitempty"`

	VariantKind   string `gorm:"column:variant_kind;type:text;not null;default:'base';index" json:"variant_kind"`
	PolicyVersion string `gorm:"column:policy_version;type:text;not null;default:'base';index" json:"policy_version"`
	SchemaVersion int    `gorm:"column:schema_version;not null;default:1" json:"schema_version"`

	ExposureKind string `gorm:"column:exposure_kind;type:text;not null;default:'base';index" json:"exposure_kind"`
	Source       string `gorm:"column:source;type:text;not null;default:'api';index" json:"source"`

	SessionID *uuid.UUID `gorm:"type:uuid;column:session_id;index" json:"session_id,omitempty"`
	TraceID   string     `gorm:"column:trace_id;type:text;index" json:"trace_id,omitempty"`
	RequestID string     `gorm:"column:request_id;type:text;index" json:"request_id,omitempty"`

	ConceptKeys  datatypes.JSON `gorm:"type:jsonb;column:concept_keys" json:"concept_keys,omitempty"`
	ConceptIDs   datatypes.JSON `gorm:"type:jsonb;column:concept_ids" json:"concept_ids,omitempty"`
	BaselineJSON datatypes.JSON `gorm:"type:jsonb;column:baseline_json" json:"baseline_json,omitempty"`

	ContentHash string         `gorm:"column:content_hash;type:text;index" json:"content_hash,omitempty"`
	Metadata    datatypes.JSON `gorm:"type:jsonb;column:metadata" json:"metadata,omitempty"`

	CreatedAt time.Time `gorm:"not null;default:now();index" json:"created_at"`
}

func (DocVariantExposure) TableName() string { return "doc_variant_exposure" }
