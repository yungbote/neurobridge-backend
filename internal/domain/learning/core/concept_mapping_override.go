package core

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ConceptMappingOverride forces a mapping for a path concept to a canonical concept.
type ConceptMappingOverride struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	PathConceptID      uuid.UUID `gorm:"type:uuid;not null;uniqueIndex;index" json:"path_concept_id"`
	CanonicalConceptID uuid.UUID `gorm:"type:uuid;not null;index" json:"canonical_concept_id"`

	Reason    string `gorm:"column:reason;type:text" json:"reason,omitempty"`
	CreatedBy string `gorm:"column:created_by;type:text" json:"created_by,omitempty"`

	CreatedAt time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (ConceptMappingOverride) TableName() string { return "concept_mapping_override" }
