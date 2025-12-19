package products

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Concept evidence (grounding).
type ConceptEvidence struct {
	ID              uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	ConceptID       uuid.UUID `gorm:"type:uuid;not null;index:idx_concept_evidence,unique,priority:1" json:"concept_id"`
	MaterialChunkID uuid.UUID `gorm:"type:uuid;not null;index:idx_concept_evidence,unique,priority:2" json:"material_chunk_id"`
	Kind            string    `gorm:"column:kind;index" json:"kind,omitempty"`
	Weight          float64   `gorm:"column:weight;not null;default:1" json:"weight"`

	CreatedAt time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (ConceptEvidence) TableName() string { return "concept_evidence" }
