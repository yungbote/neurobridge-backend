package types

import (
	"time"
	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type LessonAsset struct {
	ID					uuid.UUID			 `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	LessonID		uuid.UUID			 `gorm:"type:uuid;not null;index" json:"lesson_id"`
	Lesson			*Lesson				 `gorm:"constraint:OnDelete:CASCADE;foreignKey:LessonID;references:ID" json:"lesson,omitempty"`
	Kind				string         `gorm:"column:kind;not null" json:"kind"`
	StorageKey	string         `gorm:"column:storage_key;not null" json:"storage_key"`
	Metadata		datatypes.JSON `gorm:"column:metadata;type:jsonb" json:"metadata"`
	CreatedAt		time.Time      `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt		time.Time      `gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt		gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (LessonAsset) TableName() string { return "lesson_asset" }










