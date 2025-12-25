package products

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

// LearningNodeDoc is the canonical, versioned doc artifact for a PathNode.
// It is intentionally separate from path_node.content_json so we can evolve schemas
// without overloading core node rows.
type LearningNodeDoc struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	UserID     uuid.UUID `gorm:"type:uuid;not null;index" json:"user_id"`
	PathID     uuid.UUID `gorm:"type:uuid;not null;index" json:"path_id"`
	PathNodeID uuid.UUID `gorm:"type:uuid;not null;uniqueIndex" json:"path_node_id"`

	SchemaVersion int            `gorm:"column:schema_version;not null" json:"schema_version"`
	DocJSON       datatypes.JSON `gorm:"type:jsonb;column:doc_json;not null" json:"doc_json"`

	// Optional plain text (for future search; keep as text for portability).
	DocText string `gorm:"column:doc_text;type:text" json:"doc_text,omitempty"`

	// Hashes for idempotency + caching.
	ContentHash string `gorm:"column:content_hash;type:text;not null;index" json:"content_hash"`
	SourcesHash string `gorm:"column:sources_hash;type:text;not null;index" json:"sources_hash"`

	CreatedAt time.Time `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt time.Time `gorm:"not null;default:now();index" json:"updated_at"`
}

func (LearningNodeDoc) TableName() string { return "learning_node_doc" }
