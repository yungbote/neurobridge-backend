package materials

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// MaterialSetEdge links two material sets in a user's library.
type MaterialSetEdge struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	UserID            uuid.UUID `gorm:"type:uuid;not null;index;index:idx_material_set_edge,unique,priority:1" json:"user_id"`
	FromMaterialSetID uuid.UUID `gorm:"type:uuid;not null;index;index:idx_material_set_edge,unique,priority:2" json:"from_material_set_id"`
	ToMaterialSetID   uuid.UUID `gorm:"type:uuid;not null;index;index:idx_material_set_edge,unique,priority:3" json:"to_material_set_id"`
	Relation          string    `gorm:"type:text;not null;default:'';index;index:idx_material_set_edge,unique,priority:4" json:"relation"`
	Strength          float64   `gorm:"type:double precision;not null;default:0" json:"strength"`
	BridgingConceptIDs datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'" json:"bridging_concept_ids"`
	Metadata           datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'" json:"metadata"`

	CreatedAt time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (MaterialSetEdge) TableName() string { return "material_set_edge" }
