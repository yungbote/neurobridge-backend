package materials

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// MaterialIntent captures the learning intent and assumed trajectory for a single material file.
type MaterialIntent struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	MaterialFileID uuid.UUID     `gorm:"type:uuid;not null;index;uniqueIndex:idx_material_intent_file" json:"material_file_id"`
	MaterialFile   *MaterialFile `gorm:"constraint:OnDelete:CASCADE;foreignKey:MaterialFileID;references:ID" json:"material_file,omitempty"`
	MaterialSetID  uuid.UUID     `gorm:"type:uuid;not null;index" json:"material_set_id"`

	FromState string `gorm:"type:text;not null;default:''" json:"from_state"`
	ToState   string `gorm:"type:text;not null;default:''" json:"to_state"`
	CoreThread string `gorm:"type:text;not null;default:''" json:"core_thread"`

	DestinationConcepts datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'" json:"destination_concepts"`
	PrerequisiteConcepts datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'" json:"prerequisite_concepts"`
	AssumedKnowledge    datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'" json:"assumed_knowledge"`

	Metadata datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'" json:"metadata"`

	CreatedAt time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (MaterialIntent) TableName() string { return "material_intent" }
