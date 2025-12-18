package learning

import (
	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/domain/user"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"time"
)

type QuizAttempt struct {
	ID         uuid.UUID      `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	UserID     uuid.UUID      `gorm:"type:uuid;not null;index" json:"user_id"`
	User       *user.User     `gorm:"constraint:OnDelete:CASCADE;foreignKey:UserID;references:ID" json:"user,omitempty"`
	LessonID   uuid.UUID      `gorm:"type:uuid;not null;index" json:"lesson_id"`
	Lesson     *Lesson        `gorm:"constraint:OnDelete:CASCADE;foreignKey:LessonID;references:ID" json:"lesson,omitempty"`
	QuestionID uuid.UUID      `gorm:"type:uuid;not null;index" json:"question_id"`
	Question   *QuizQuestion  `gorm:"constraint:OnDelete:CASCADE;foreignKey:QuestionID;references:ID" json:"question,omitempty"`
	IsCorrect  bool           `gorm:"column:is_correct;not null" json:"is_correct"`
	Answer     datatypes.JSON `gorm:"type:jsonb;column:answer" json:"answer"`
	Metadata   datatypes.JSON `gorm:"type:jsonb;column:metadata" json:"metadata"`
	CreatedAt  time.Time      `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt  time.Time      `gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt  gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (QuizAttempt) TableName() string { return "quiz_attempt" }
