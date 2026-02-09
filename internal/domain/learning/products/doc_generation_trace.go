package products

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

// DocGenerationTrace captures deterministic inputs and outputs for a generated doc variant.
type DocGenerationTrace struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	TraceID string `gorm:"column:trace_id;type:text;not null;uniqueIndex" json:"trace_id"`

	UserID     uuid.UUID `gorm:"type:uuid;not null;index" json:"user_id"`
	PathID     uuid.UUID `gorm:"type:uuid;not null;index" json:"path_id"`
	PathNodeID uuid.UUID `gorm:"type:uuid;not null;index" json:"path_node_id"`

	PolicyVersion    string `gorm:"column:policy_version;type:text;not null;index" json:"policy_version"`
	SchemaVersion    int    `gorm:"column:schema_version;not null" json:"schema_version"`
	Model            string `gorm:"column:model;type:text" json:"model,omitempty"`
	PromptHash       string `gorm:"column:prompt_hash;type:text;index" json:"prompt_hash,omitempty"`
	RetrievalPackID  string `gorm:"column:retrieval_pack_id;type:text;index" json:"retrieval_pack_id,omitempty"`
	BlueprintVersion string `gorm:"column:blueprint_version;type:text;index" json:"blueprint_version,omitempty"`

	TraceJSON datatypes.JSON `gorm:"type:jsonb;column:trace_json;not null" json:"trace_json"`

	CreatedAt time.Time `gorm:"not null;default:now();index" json:"created_at"`
}

func (DocGenerationTrace) TableName() string { return "doc_generation_trace" }
