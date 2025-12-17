package types

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type LessonCitation struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	LessonID uuid.UUID `gorm:"type:uuid;not null;index:idx_lesson_citation,unique,priority:1" json:"lesson_id"`
	Lesson   *Lesson   `gorm:"constraint:OnDelete:CASCADE;foreignKey:LessonID;references:ID" json:"lesson,omitempty"`

	MaterialChunkID uuid.UUID     `gorm:"type:uuid;not null;index:idx_lesson_citation,unique,priority:2" json:"material_chunk_id"`
	MaterialChunk   *MaterialChunk `gorm:"constraint:OnDelete:CASCADE;foreignKey:MaterialChunkID;references:ID" json:"material_chunk,omitempty"`

	CreatedAt time.Time      `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (LessonCitation) TableName() string { return "lesson_citation" }










