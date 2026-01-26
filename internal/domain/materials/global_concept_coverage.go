package materials

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// GlobalConceptCoverage aggregates concept coverage across all material sets for a user.
type GlobalConceptCoverage struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	UserID          uuid.UUID `gorm:"type:uuid;not null;index;index:idx_global_concept_coverage,unique,priority:1" json:"user_id"`
	GlobalConceptID uuid.UUID `gorm:"type:uuid;not null;index;index:idx_global_concept_coverage,unique,priority:2" json:"global_concept_id"`

	MaterialSetIDs    datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'" json:"material_set_ids"`
	CoverageDepth     float64        `gorm:"type:double precision;not null;default:0" json:"coverage_depth"`
	ExposureScore     float64        `gorm:"type:double precision;not null;default:0" json:"exposure_score"`
	CrossSetRelevance float64        `gorm:"type:double precision;not null;default:0" json:"cross_set_relevance"`
	Metadata          datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'" json:"metadata"`

	CreatedAt time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (GlobalConceptCoverage) TableName() string { return "global_concept_coverage" }
