package products

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// Concept edges (graph).
type ConceptEdge struct {
	ID            uuid.UUID      `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	FromConceptID uuid.UUID      `gorm:"type:uuid;not null;index:idx_concept_edge,unique,priority:1" json:"from_concept_id"`
	ToConceptID   uuid.UUID      `gorm:"type:uuid;not null;index:idx_concept_edge,unique,priority:2" json:"to_concept_id"`
	EdgeType      string         `gorm:"column:edge_type;not null;index:idx_concept_edge,unique,priority:3" json:"edge_type"`
	Strength      float64        `gorm:"column:strength;not null;default:1" json:"strength"`
	Evidence      datatypes.JSON `gorm:"column:evidence;type:jsonb" json:"evidence"`

	CreatedAt time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (ConceptEdge) TableName() string { return "concept_edge" }
