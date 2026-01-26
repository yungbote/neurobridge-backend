package materials

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// MaterialEdge links two material files within a set (prerequisite, reinforces, etc.).
type MaterialEdge struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	MaterialSetID     uuid.UUID     `gorm:"type:uuid;not null;index;index:idx_material_edge_set_pair,unique,priority:1" json:"material_set_id"`
	FromMaterialFileID uuid.UUID     `gorm:"type:uuid;not null;index;index:idx_material_edge_set_pair,unique,priority:2" json:"from_material_file_id"`
	ToMaterialFileID   uuid.UUID     `gorm:"type:uuid;not null;index;index:idx_material_edge_set_pair,unique,priority:3" json:"to_material_file_id"`
	EdgeType          string        `gorm:"type:text;not null;default:'';index;index:idx_material_edge_set_pair,unique,priority:4" json:"edge_type"`
	Strength          float64       `gorm:"type:double precision;not null;default:0" json:"strength"`
	BridgingConcepts  datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'" json:"bridging_concepts"`
	Metadata          datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'" json:"metadata"`

	CreatedAt time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (MaterialEdge) TableName() string { return "material_edge" }
