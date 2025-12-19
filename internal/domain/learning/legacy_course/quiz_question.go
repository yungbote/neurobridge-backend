package legacy_course

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type QuizQuestion struct {
	ID            uuid.UUID      `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	LessonID      uuid.UUID      `gorm:"type:uuid;not null;index" json:"lesson_id"`
	Lesson        *Lesson        `gorm:"constraint:OnDelete:CASCADE;foreignKey:LessonID;references:ID" json:"lesson,omitempty"`
	Index         int            `gorm:"column:index;not null" json:"index"`
	Type          string         `gorm:"column:type;not null" json:"type"`
	PromptMD      string         `gorm:"column:prompt_md;not null" json:"prompt_md"`
	Options       datatypes.JSON `gorm:"column:options;type:jsonb" json:"options"`
	CorrectAnswer datatypes.JSON `gorm:"column:correct_answer;type:jsonb" json:"correct_answer"`
	ExplanationMD string         `gorm:"column:explanation_md" json:"explanation_md"`
	Metadata      datatypes.JSON `gorm:"column:metadata;type:jsonb" json:"metadata"`

	CreatedAt time.Time      `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (QuizQuestion) TableName() string { return "quiz_question" }
