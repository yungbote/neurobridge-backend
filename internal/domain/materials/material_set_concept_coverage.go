package materials

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// MaterialSetConceptCoverage captures how a concept is covered within a material set.
type MaterialSetConceptCoverage struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	MaterialSetID uuid.UUID  `gorm:"type:uuid;not null;index;index:idx_material_set_concept,unique,priority:1" json:"material_set_id"`
	PathID        *uuid.UUID `gorm:"type:uuid;index" json:"path_id,omitempty"`

	ConceptKey         string     `gorm:"type:text;not null;index;index:idx_material_set_concept,unique,priority:2" json:"concept_key"`
	CanonicalConceptID *uuid.UUID `gorm:"type:uuid;index" json:"canonical_concept_id,omitempty"`

	CoverageType string  `gorm:"type:text;not null;default:'';index" json:"coverage_type"`
	Depth        string  `gorm:"type:text;not null;default:''" json:"depth"`
	Score        float64 `gorm:"type:double precision;not null;default:0" json:"score"`

	SourceMaterialFileIDs datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'" json:"source_material_file_ids"`
	Metadata              datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'" json:"metadata"`

	CreatedAt time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (MaterialSetConceptCoverage) TableName() string { return "material_set_concept_coverage" }
