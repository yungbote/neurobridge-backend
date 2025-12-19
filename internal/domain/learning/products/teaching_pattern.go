package products

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type TeachingPattern struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	// Stable key so you can reference patterns in prompts + retrieval
	PatternKey string `gorm:"column:pattern_key;not null;uniqueIndex" json:"pattern_key"`

	Name      string `gorm:"column:name;not null" json:"name"`
	WhenToUse string `gorm:"column:when_to_use;type:text" json:"when_to_use"`

	// JSON blob holding your "representation" object (primary_modality, study_cycle, etc.)
	PatternSpec datatypes.JSON `gorm:"column:pattern_spec;type:jsonb" json:"pattern_spec"`

	// Optional local embedding + external vector ref
	Embedding datatypes.JSON `gorm:"column:embedding;type:jsonb" json:"embedding"`
	VectorID  string         `gorm:"column:vector_id;index" json:"vector_id,omitempty"`

	CreatedAt time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (TeachingPattern) TableName() string { return "teaching_pattern" }










