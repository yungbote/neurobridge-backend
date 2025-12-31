package products

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

// LearningNodeDocRevision records block-level changes for generated node docs.
type LearningNodeDocRevision struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	DocID      uuid.UUID  `gorm:"type:uuid;not null;index" json:"doc_id"`
	JobID      *uuid.UUID `gorm:"type:uuid;index" json:"job_id,omitempty"`
	UserID     uuid.UUID  `gorm:"type:uuid;not null;index" json:"user_id"`
	PathID     uuid.UUID  `gorm:"type:uuid;not null;index" json:"path_id"`
	PathNodeID uuid.UUID  `gorm:"type:uuid;not null;index" json:"path_node_id"`

	BlockID   string `gorm:"column:block_id;type:text;not null" json:"block_id"`
	BlockType string `gorm:"column:block_type;type:text;not null" json:"block_type"`

	Operation      string         `gorm:"column:operation;type:text;not null;index" json:"operation"`
	CitationPolicy string         `gorm:"column:citation_policy;type:text;not null" json:"citation_policy"`
	Instruction    string         `gorm:"column:instruction;type:text" json:"instruction,omitempty"`
	Selection      datatypes.JSON `gorm:"type:jsonb;column:selection" json:"selection,omitempty"`

	BeforeJSON datatypes.JSON `gorm:"type:jsonb;column:before_json;not null" json:"before_json"`
	AfterJSON  datatypes.JSON `gorm:"type:jsonb;column:after_json;not null" json:"after_json"`

	Status        string `gorm:"column:status;type:text;not null;index" json:"status"`
	Error         string `gorm:"column:error;type:text" json:"error,omitempty"`
	Model         string `gorm:"column:model;type:text" json:"model,omitempty"`
	PromptVersion string `gorm:"column:prompt_version;type:text" json:"prompt_version,omitempty"`
	TokensIn      int    `gorm:"column:tokens_in;not null" json:"tokens_in"`
	TokensOut     int    `gorm:"column:tokens_out;not null" json:"tokens_out"`

	CreatedAt time.Time `gorm:"not null;default:now();index" json:"created_at"`
}

func (LearningNodeDocRevision) TableName() string { return "learning_node_doc_revision" }
