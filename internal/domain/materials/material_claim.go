package materials

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// MaterialClaim is an atomic, grounded statement extracted from a MaterialSet.
// Claims are referenced by evidence chunks and can be linked to entities/concepts.
type MaterialClaim struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	MaterialSetID uuid.UUID    `gorm:"type:uuid;not null;index;index:idx_material_claim_set_key,unique,priority:1" json:"material_set_id"`
	MaterialSet   *MaterialSet `gorm:"constraint:OnDelete:CASCADE;foreignKey:MaterialSetID;references:ID" json:"material_set,omitempty"`

	// Key is a stable normalized identifier (e.g., a content hash) used for idempotent upserts.
	Key string `gorm:"type:text;not null;index:idx_material_claim_set_key,unique,priority:2" json:"key"`

	Kind       string         `gorm:"type:text;not null;default:'claim';index" json:"kind"`
	Content    string         `gorm:"type:text;not null" json:"content"`
	Confidence float64        `gorm:"not null;default:0.7" json:"confidence"`
	Metadata   datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'" json:"metadata"`

	CreatedAt time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (MaterialClaim) TableName() string { return "material_claim" }
