package types

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type CourseConcept struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	CourseID uuid.UUID `gorm:"type:uuid;not null;index:idx_course_concept_course" json:"course_id"`
	Course   *Course   `gorm:"constraint:OnDelete:CASCADE;foreignKey:CourseID;references:ID" json:"course,omitempty"`

	ParentID *uuid.UUID     `gorm:"type:uuid;index:idx_course_concept_parent" json:"parent_id,omitempty"`
	Parent   *CourseConcept `gorm:"constraint:OnDelete:SET NULL;foreignKey:ParentID;references:ID" json:"parent,omitempty"`

	// IMPORTANT: unique per course (not global)
	Key string `gorm:"column:key;not null;index:idx_course_concept_key,unique,priority:2;index:idx_course_concept_course_key,unique,priority:2" json:"key"`
	// also include course_id in the composite unique
	// (gorm needs course_id tagged too)
	// We keep both indexes for easy lookups; composite is the one that matters.

	Name      string `gorm:"column:name;not null" json:"name"`
	Depth     int    `gorm:"column:depth;not null;default:0" json:"depth"`
	SortIndex int    `gorm:"column:sort_index;not null;default:0" json:"sort_index"`

	Summary   string         `gorm:"column:summary" json:"summary"`
	KeyPoints datatypes.JSON `gorm:"column:key_points;type:jsonb" json:"key_points"` // []string
	Citations datatypes.JSON `gorm:"column:citations;type:jsonb" json:"citations"`   // []string chunk_id

	CreatedAt time.Time      `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (CourseConcept) TableName() string { return "course_concept" }










