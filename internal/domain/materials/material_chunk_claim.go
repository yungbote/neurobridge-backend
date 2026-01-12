package materials

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type MaterialChunkClaim struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	MaterialChunkID uuid.UUID     `gorm:"type:uuid;not null;index:idx_material_chunk_claim,unique,priority:1" json:"material_chunk_id"`
	MaterialChunk   *MaterialChunk `gorm:"constraint:OnDelete:CASCADE;foreignKey:MaterialChunkID;references:ID" json:"material_chunk,omitempty"`

	MaterialClaimID uuid.UUID     `gorm:"type:uuid;not null;index:idx_material_chunk_claim,unique,priority:2" json:"material_claim_id"`
	MaterialClaim   *MaterialClaim `gorm:"constraint:OnDelete:CASCADE;foreignKey:MaterialClaimID;references:ID" json:"material_claim,omitempty"`

	Relation string  `gorm:"type:text;not null;default:'supports';index" json:"relation"`
	Weight   float64 `gorm:"not null;default:1" json:"weight"`

	CreatedAt time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (MaterialChunkClaim) TableName() string { return "material_chunk_claim" }

