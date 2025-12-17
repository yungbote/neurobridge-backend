package types

import (
	"time"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type ActivityConcept struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	ActivityID uuid.UUID `gorm:"type:uuid;not null;index:idx_activity_concept,unique,priority:1" json:"activity_id"`
	Activity   *Activity `gorm:"constraint:OnDelete:CASCADE;foreignKey:ActivityID;references:ID" json:"activity,omitempty"`
	ConceptID uuid.UUID `gorm:"type:uuid;not null;index:idx_activity_concept,unique,priority:2" json:"concept_id"`
	Concept   *Concept  `gorm:"constraint:OnDelete:CASCADE;foreignKey:ConceptID;references:ID" json:"concept,omitempty"`
	Role   string  `gorm:"column:role;not null;default:'primary';index" json:"role"` // primary|secondary|prereq
	Weight float64 `gorm:"column:weight;not null;default:1" json:"weight"`
	CreatedAt time.Time      `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (ActivityConcept) TableName() string { return "activity_concept" }










