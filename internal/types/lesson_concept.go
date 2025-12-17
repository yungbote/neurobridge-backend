package types

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type LessonConcept struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	LessonID uuid.UUID `gorm:"type:uuid;not null;index:idx_lesson_concept,unique,priority:1" json:"lesson_id"`
	Lesson   *Lesson   `gorm:"constraint:OnDelete:CASCADE;foreignKey:LessonID;references:ID" json:"lesson,omitempty"`

	CourseConceptID uuid.UUID      `gorm:"type:uuid;not null;index:idx_lesson_concept,unique,priority:2" json:"course_concept_id"`
	CourseConcept   *CourseConcept `gorm:"constraint:OnDelete:CASCADE;foreignKey:CourseConceptID;references:ID" json:"course_concept,omitempty"`

	CreatedAt time.Time      `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (LessonConcept) TableName() string { return "lesson_concept" }










