package products

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

// DocProbe records a selected assessment probe for a node doc.
type DocProbe struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	UserID     uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:idx_doc_probe_user_node_block,priority:1;index" json:"user_id"`
	PathID     uuid.UUID `gorm:"type:uuid;not null;index" json:"path_id"`
	PathNodeID uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:idx_doc_probe_user_node_block,priority:2;index" json:"path_node_id"`

	BlockID   string `gorm:"column:block_id;type:text;not null;uniqueIndex:idx_doc_probe_user_node_block,priority:3" json:"block_id"`
	BlockType string `gorm:"column:block_type;type:text;not null;index" json:"block_type"`
	ProbeKind string `gorm:"column:probe_kind;type:text;not null;index" json:"probe_kind"`

	ConceptKeys          datatypes.JSON `gorm:"type:jsonb;column:concept_keys" json:"concept_keys,omitempty"`
	ConceptIDs           datatypes.JSON `gorm:"type:jsonb;column:concept_ids" json:"concept_ids,omitempty"`
	TriggerAfterBlockIDs datatypes.JSON `gorm:"type:jsonb;column:trigger_after_block_ids" json:"trigger_after_block_ids,omitempty"`

	InfoGain float64 `gorm:"column:info_gain;not null;default:0" json:"info_gain"`
	Score    float64 `gorm:"column:score;not null;default:0" json:"score"`

	PolicyVersion string `gorm:"column:policy_version;type:text;not null;index" json:"policy_version"`
	SchemaVersion int    `gorm:"column:schema_version;not null;default:1" json:"schema_version"`
	Status        string `gorm:"column:status;type:text;not null;default:'planned';index" json:"status"`

	ShownCount  int        `gorm:"column:shown_count;not null;default:0" json:"shown_count"`
	ShownAt     *time.Time `gorm:"column:shown_at;index" json:"shown_at,omitempty"`
	CompletedAt *time.Time `gorm:"column:completed_at;index" json:"completed_at,omitempty"`
	DismissedAt *time.Time `gorm:"column:dismissed_at;index" json:"dismissed_at,omitempty"`

	Metadata datatypes.JSON `gorm:"type:jsonb;column:metadata" json:"metadata,omitempty"`

	CreatedAt time.Time `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time `gorm:"not null;default:now();index" json:"updated_at"`
}

func (DocProbe) TableName() string { return "doc_probe" }
