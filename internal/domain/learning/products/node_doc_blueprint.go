package products

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

// LearningNodeDocBlueprint stores immutable blueprint constraints for a node doc.
type LearningNodeDocBlueprint struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	PathID     uuid.UUID `gorm:"type:uuid;not null;index" json:"path_id"`
	PathNodeID uuid.UUID `gorm:"type:uuid;not null;index:idx_node_doc_blueprint,unique,priority:1" json:"path_node_id"`

	BlueprintVersion string `gorm:"column:blueprint_version;type:text;not null;index:idx_node_doc_blueprint,unique,priority:2" json:"blueprint_version"`
	SchemaVersion    int    `gorm:"column:schema_version;not null" json:"schema_version"`

	BlueprintJSON datatypes.JSON `gorm:"type:jsonb;column:blueprint_json;not null" json:"blueprint_json"`

	CreatedAt time.Time `gorm:"not null;default:now();index" json:"created_at"`
}

func (LearningNodeDocBlueprint) TableName() string { return "learning_node_doc_blueprint" }
