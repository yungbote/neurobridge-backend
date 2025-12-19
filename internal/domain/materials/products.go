package materials

import (
	"time"
	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type MaterialSetSummary struct {
	ID            uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	MaterialSetID uuid.UUID `gorm:"type:uuid;not null;uniqueIndex" json:"material_set_id"`
	UserID        uuid.UUID `gorm:"type:uuid;not null;index" json:"user_id"`

	Subject string `gorm:"column:subject;index" json:"subject,omitempty"`
	Level   string `gorm:"column:level;index" json:"level,omitempty"`

	SummaryMD string `gorm:"column:summary_md;type:text" json:"summary_md"`
	Tags      datatypes.JSON `gorm:"column:tags;type:jsonb" json:"tags"` // []string

	ConceptKeys datatypes.JSON `gorm:"column:concept_keys;type:jsonb" json:"concept_keys"` // []string

	Embedding datatypes.JSON `gorm:"column:embedding;type:jsonb" json:"embedding"` // []float32
	VectorID  string         `gorm:"column:vector_id;index" json:"vector_id,omitempty"`

	CreatedAt time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (MaterialSetSummary) TableName() string { return "material_set_summary" }










