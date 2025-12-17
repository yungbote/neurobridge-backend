package types

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type LessonVariant struct {
	ID       uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	LessonID uuid.UUID `gorm:"type:uuid;not null;index:idx_lesson_variant,unique,priority:1" json:"lesson_id"`
	Lesson   *Lesson   `gorm:"constraint:OnDelete:CASCADE;foreignKey:LessonID;references:ID" json:"lesson,omitempty"`

	Variant   string `gorm:"column:variant;not null;index:idx_lesson_variant,unique,priority:2" json:"variant"` // concise|full
	ContentMD string `gorm:"column:content_md;type:text;not null" json:"content_md"`

	CreatedAt time.Time      `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (LessonVariant) TableName() string { return "lesson_variant" }










