package products

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

// LearningNodeDocVariant stores user- or cohort-specific variants of a node doc.
// Variants are immutable artifacts with full traceability.
type LearningNodeDocVariant struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	UserID     uuid.UUID `gorm:"type:uuid;not null;index" json:"user_id"`
	PathID     uuid.UUID `gorm:"type:uuid;not null;index" json:"path_id"`
	PathNodeID uuid.UUID `gorm:"type:uuid;not null;index" json:"path_node_id"`

	BaseDocID *uuid.UUID `gorm:"type:uuid;column:base_doc_id;index" json:"base_doc_id,omitempty"`

	VariantKind   string `gorm:"column:variant_kind;type:text;not null;index" json:"variant_kind"`
	PolicyVersion string `gorm:"column:policy_version;type:text;not null;index" json:"policy_version"`
	SchemaVersion int    `gorm:"column:schema_version;not null" json:"schema_version"`

	SnapshotID      string `gorm:"column:snapshot_id;type:text;not null;uniqueIndex" json:"snapshot_id"`
	RetrievalPackID string `gorm:"column:retrieval_pack_id;type:text;index" json:"retrieval_pack_id,omitempty"`
	TraceID         string `gorm:"column:trace_id;type:text;index" json:"trace_id,omitempty"`

	DocJSON datatypes.JSON `gorm:"type:jsonb;column:doc_json;not null" json:"doc_json"`
	DocText string         `gorm:"column:doc_text;type:text" json:"doc_text,omitempty"`

	ContentHash string `gorm:"column:content_hash;type:text;not null;index" json:"content_hash"`
	SourcesHash string `gorm:"column:sources_hash;type:text;not null;index" json:"sources_hash"`

	Status    string     `gorm:"column:status;type:text;not null;default:'active';index" json:"status"`
	ExpiresAt *time.Time `gorm:"column:expires_at;index" json:"expires_at,omitempty"`

	CreatedAt time.Time `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time `gorm:"not null;default:now();index" json:"updated_at"`
}

func (LearningNodeDocVariant) TableName() string { return "learning_node_doc_variant" }
