package types

import (
	"time"
	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type UserEvent struct {
	ID				uuid.UUID			 `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	UserID		uuid.UUID			 `gorm:"type:uuid;not null;index" json:"user_id"`
	User			*User					 `gorm:"constraint:OnDelete:CASCADE;foreignKey:UserID;references:ID" json:"user,omitempty"`
	CourseID	*uuid.UUID		 `gorm:"type:uuid;index" json:"course_id,omitempty"`
	Course		*Course				 `gorm:"constraint:OnDelete:SET NULL;foreignKey:CourseID;references:ID" json:"course,omitempty"`
	LessonID	*uuid.UUID		 `gorm:"type:uuid;index" json:"lesson_id,omitempty"`
	Lesson		*Lesson				 `gorm:"constraint:OnDelete:SET NULL;foreignKey:LessonID;references:ID" json:"lesson,omitempty"`
	Type			string         `gorm:"column:type;not null;index" json:"type"`
	Data			datatypes.JSON `gorm:"type:jsonb;column:data" json:"data"`
	CreatedAt time.Time      `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (UserEvent) TableName() string { return "user_event" }










