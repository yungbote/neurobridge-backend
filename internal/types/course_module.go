package types

import (
	"time"
	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type CourseModule struct {
	ID					uuid.UUID				`gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	CourseID		uuid.UUID				`gorm:"type:uuid;not null;index" json:"course_id"`
	Course			*Course					`gorm:"constraint:OnDelete:CASCADE;foreignKey:CourseID;references:ID" json:"course,omitempty"`
	Index       int							`gorm:"column:index;not null" json:"index"`
	Title       string					`gorm:"column:title;not null" json:"title"`
	Description string					`gorm:"column:description" json:"description"`
	Metadata    datatypes.JSON	`gorm:"column:metadata;type:jsonb" json:"metadata"`
	CreatedAt		time.Time				`gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt		time.Time				`gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt		gorm.DeletedAt	`gorm:"index" json:"deleted_at,omitempty"`
}

func (CourseModule) TableName() string { return "course_module" }










