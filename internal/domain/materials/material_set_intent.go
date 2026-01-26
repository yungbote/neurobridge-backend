package materials

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// MaterialSetIntent captures the collective intent and structure of a material set.
type MaterialSetIntent struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	MaterialSetID uuid.UUID    `gorm:"type:uuid;not null;uniqueIndex:idx_material_set_intent" json:"material_set_id"`
	MaterialSet   *MaterialSet `gorm:"constraint:OnDelete:CASCADE;foreignKey:MaterialSetID;references:ID" json:"material_set,omitempty"`

	FromState  string `gorm:"type:text;not null;default:''" json:"from_state"`
	ToState    string `gorm:"type:text;not null;default:''" json:"to_state"`
	CoreThread string `gorm:"type:text;not null;default:''" json:"core_thread"`

	SpineMaterialFileIDs    datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'" json:"spine_material_file_ids"`
	SatelliteMaterialFileIDs datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'" json:"satellite_material_file_ids"`
	GapsConceptKeys         datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'" json:"gaps_concept_keys"`
	RedundancyNotes         datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'" json:"redundancy_notes"`
	ConflictNotes           datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'" json:"conflict_notes"`

	Metadata datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'" json:"metadata"`

	CreatedAt time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (MaterialSetIntent) TableName() string { return "material_set_intent" }
