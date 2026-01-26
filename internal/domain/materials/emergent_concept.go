package materials

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// EmergentConcept captures concepts that emerge only from combinations of material sets.
type EmergentConcept struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	UserID uuid.UUID `gorm:"type:uuid;not null;index;index:idx_emergent_concept_user_key,unique,priority:1" json:"user_id"`
	Key    string    `gorm:"type:text;not null;index:idx_emergent_concept_user_key,unique,priority:2" json:"key"`

	Name        string `gorm:"type:text;not null;default:''" json:"name"`
	Summary     string `gorm:"type:text;not null;default:''" json:"summary"`

	SourceMaterialSetIDs datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'" json:"source_material_set_ids"`
	PrereqConceptIDs     datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'" json:"prereq_concept_ids"`
	Metadata             datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'" json:"metadata"`

	CreatedAt time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (EmergentConcept) TableName() string { return "emergent_concept" }
