package types

import (
	"time"
	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type LessonProgress struct {
	ID							 uuid.UUID			`gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	UserID				   uuid.UUID			`gorm:"type:uuid;not null;index:idx_user_lesson,unique" json:"user_id"`
	User						 *User					`gorm:"constraint:OnDelete:CASCADE;foreignKey:UserID;references:ID" json:"user,omitempty"`
	LessonID				 uuid.UUID			`gorm:"type:uuid;not null;index:idx_user_lesson,unique" json:"lesson_id"`
	Lesson					 *Lesson				`gorm:"constraint:OnDelete:CASCADE;foreignKey:LessonID;references:ID" json:"lesson,omitempty"`
	Status           string					`gorm:"column:status;not null;default:'not_started'" json:"status"`
	LastOpenedAt     *time.Time			`gorm:"column:last_opened_at" json:"last_opened_at,omitempty"`
	CompletedAt      *time.Time			`gorm:"column:completed_at" json:"completed_at,omitempty"`
	TimeSpentSeconds int						`gorm:"column:time_spent_seconds;not null;default:0" json:"time_spent_seconds"`
	Metadata         datatypes.JSON `gorm:"type:jsonb;column:metadata" json:"metadata"`
	CreatedAt				 time.Time      `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt				 time.Time      `gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt				 gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (LessonProgress) TableName() string { return "lesson_progress" }










