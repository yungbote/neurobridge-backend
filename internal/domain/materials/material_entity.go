package materials

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// MaterialEntity is a normalized entity extracted from a MaterialSet's source chunks.
// It is derived data intended for GraphRAG + explainability.
type MaterialEntity struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	MaterialSetID uuid.UUID    `gorm:"type:uuid;not null;index;index:idx_material_entity_set_key,unique,priority:1" json:"material_set_id"`
	MaterialSet   *MaterialSet `gorm:"constraint:OnDelete:CASCADE;foreignKey:MaterialSetID;references:ID" json:"material_set,omitempty"`

	// Key is a stable normalized identifier (e.g., lowercased name) used for idempotent upserts.
	Key string `gorm:"type:text;not null;index:idx_material_entity_set_key,unique,priority:2" json:"key"`

	Name        string         `gorm:"type:text;not null;index" json:"name"`
	Type        string         `gorm:"type:text;not null;default:'unknown';index" json:"type"`
	Description string         `gorm:"type:text;not null;default:''" json:"description"`
	Aliases     datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'" json:"aliases"`
	Metadata    datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'" json:"metadata"`

	CreatedAt time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (MaterialEntity) TableName() string { return "material_entity" }

