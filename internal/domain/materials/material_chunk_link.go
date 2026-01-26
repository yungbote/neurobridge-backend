package materials

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// MaterialChunkLink links chunks across materials (redundant, reinforces, bridge).
type MaterialChunkLink struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	MaterialSetID uuid.UUID `gorm:"type:uuid;not null;index;index:idx_material_chunk_link,unique,priority:1" json:"material_set_id"`
	FromChunkID   uuid.UUID `gorm:"type:uuid;not null;index;index:idx_material_chunk_link,unique,priority:2" json:"from_chunk_id"`
	ToChunkID     uuid.UUID `gorm:"type:uuid;not null;index;index:idx_material_chunk_link,unique,priority:3" json:"to_chunk_id"`
	Relation      string    `gorm:"type:text;not null;default:'';index" json:"relation"`
	Strength      float64   `gorm:"type:double precision;not null;default:0" json:"strength"`
	Metadata      datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'" json:"metadata"`

	CreatedAt time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (MaterialChunkLink) TableName() string { return "material_chunk_link" }
