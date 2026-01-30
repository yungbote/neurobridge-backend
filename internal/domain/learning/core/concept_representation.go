package core

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// ConceptRepresentation stores mapping + representation facets for a path concept.
type ConceptRepresentation struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	PathConceptID      uuid.UUID  `gorm:"type:uuid;not null;uniqueIndex;index" json:"path_concept_id"`
	CanonicalConceptID uuid.UUID  `gorm:"type:uuid;not null;index" json:"canonical_concept_id"`
	PathID             *uuid.UUID `gorm:"type:uuid;column:path_id;index" json:"path_id,omitempty"`

	RepresentationFacets  datatypes.JSON `gorm:"column:representation_facets;type:jsonb" json:"representation_facets,omitempty"`
	RepresentationSummary string         `gorm:"column:representation_summary;type:text" json:"representation_summary,omitempty"`
	RepresentationAliases datatypes.JSON `gorm:"column:representation_aliases;type:jsonb" json:"representation_aliases,omitempty"`

	MappingConfidence float64 `gorm:"column:mapping_confidence;not null;default:0" json:"mapping_confidence"`
	MappingMethod     string  `gorm:"column:mapping_method;type:text" json:"mapping_method"`

	CreatedAt time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (ConceptRepresentation) TableName() string { return "concept_representation" }
