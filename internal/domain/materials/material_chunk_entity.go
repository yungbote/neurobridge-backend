package materials

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type MaterialChunkEntity struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	MaterialChunkID uuid.UUID     `gorm:"type:uuid;not null;index:idx_material_chunk_entity,unique,priority:1" json:"material_chunk_id"`
	MaterialChunk   *MaterialChunk `gorm:"constraint:OnDelete:CASCADE;foreignKey:MaterialChunkID;references:ID" json:"material_chunk,omitempty"`

	MaterialEntityID uuid.UUID     `gorm:"type:uuid;not null;index:idx_material_chunk_entity,unique,priority:2" json:"material_entity_id"`
	MaterialEntity   *MaterialEntity `gorm:"constraint:OnDelete:CASCADE;foreignKey:MaterialEntityID;references:ID" json:"material_entity,omitempty"`

	Relation string  `gorm:"type:text;not null;default:'mentions';index" json:"relation"`
	Weight   float64 `gorm:"not null;default:1" json:"weight"`

	CreatedAt time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (MaterialChunkEntity) TableName() string { return "material_chunk_entity" }

