package materials

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type MaterialClaimEntity struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	MaterialClaimID uuid.UUID     `gorm:"type:uuid;not null;index:idx_material_claim_entity,unique,priority:1" json:"material_claim_id"`
	MaterialClaim   *MaterialClaim `gorm:"constraint:OnDelete:CASCADE;foreignKey:MaterialClaimID;references:ID" json:"material_claim,omitempty"`

	MaterialEntityID uuid.UUID     `gorm:"type:uuid;not null;index:idx_material_claim_entity,unique,priority:2" json:"material_entity_id"`
	MaterialEntity   *MaterialEntity `gorm:"constraint:OnDelete:CASCADE;foreignKey:MaterialEntityID;references:ID" json:"material_entity,omitempty"`

	Relation string  `gorm:"type:text;not null;default:'about';index" json:"relation"`
	Weight   float64 `gorm:"not null;default:1" json:"weight"`

	CreatedAt time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (MaterialClaimEntity) TableName() string { return "material_claim_entity" }

