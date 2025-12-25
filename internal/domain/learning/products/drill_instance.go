package products

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

// LearningDrillInstance caches a generated drill payload keyed by inputs (sources_hash) and parameters.
// Drills are supplemental tools launched inline from a node doc.
type LearningDrillInstance struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	UserID     uuid.UUID `gorm:"type:uuid;not null;index:idx_learning_drill_instance_key,unique,priority:1;index" json:"user_id"`
	PathID     uuid.UUID `gorm:"type:uuid;not null;index" json:"path_id"`
	PathNodeID uuid.UUID `gorm:"type:uuid;not null;index:idx_learning_drill_instance_key,unique,priority:2;index" json:"path_node_id"`

	Kind        string `gorm:"column:kind;type:text;not null;index:idx_learning_drill_instance_key,unique,priority:3;index" json:"kind"`
	Count       int    `gorm:"column:count;not null;index:idx_learning_drill_instance_key,unique,priority:4" json:"count"`
	SourcesHash string `gorm:"column:sources_hash;type:text;not null;index:idx_learning_drill_instance_key,unique,priority:5" json:"sources_hash"`

	SchemaVersion int            `gorm:"column:schema_version;not null" json:"schema_version"`
	PayloadJSON   datatypes.JSON `gorm:"type:jsonb;column:payload_json;not null" json:"payload_json"`

	ContentHash string `gorm:"column:content_hash;type:text;not null;index" json:"content_hash"`

	CreatedAt time.Time `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time `gorm:"not null;default:now();index" json:"updated_at"`
}

func (LearningDrillInstance) TableName() string { return "learning_drill_instance" }
