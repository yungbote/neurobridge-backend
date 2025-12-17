package types

import (
	"time"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type CourseTag struct {
	ID       uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	CourseID uuid.UUID `gorm:"type:uuid;not null;index:idx_course_tag,unique,priority:1" json:"course_id"`
	Tag      string    `gorm:"column:tag;not null;index:idx_course_tag,unique,priority:2" json:"tag"`

	CreatedAt time.Time      `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}


func (CourseTag) TableName() string { return "course_tag" }










