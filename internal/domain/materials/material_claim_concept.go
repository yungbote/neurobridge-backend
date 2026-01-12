package materials

import (
	"time"

	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/domain/learning/core"
	"gorm.io/gorm"
)

type MaterialClaimConcept struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	MaterialClaimID uuid.UUID      `gorm:"type:uuid;not null;index:idx_material_claim_concept,unique,priority:1" json:"material_claim_id"`
	MaterialClaim   *MaterialClaim `gorm:"constraint:OnDelete:CASCADE;foreignKey:MaterialClaimID;references:ID" json:"material_claim,omitempty"`

	ConceptID uuid.UUID    `gorm:"type:uuid;not null;index:idx_material_claim_concept,unique,priority:2" json:"concept_id"`
	Concept   *core.Concept `gorm:"constraint:OnDelete:CASCADE;foreignKey:ConceptID;references:ID" json:"concept,omitempty"`

	Relation string  `gorm:"type:text;not null;default:'about';index" json:"relation"`
	Weight   float64 `gorm:"not null;default:1" json:"weight"`

	CreatedAt time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (MaterialClaimConcept) TableName() string { return "material_claim_concept" }

