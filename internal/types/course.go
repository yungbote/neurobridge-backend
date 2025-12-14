package types

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type Course struct {
	ID            uuid.UUID				`gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	UserID        uuid.UUID				`gorm:"type:uuid;not null;index" json:"user_id"`
	User          *User						`gorm:"constraint:OnDelete:CASCADE;foreignKey:UserID;references:ID" json:"user,omitempty"`
	MaterialSetID *uuid.UUID			`gorm:"type:uuid;index" json:"material_set_id,omitempty"`
	MaterialSet   *MaterialSet		`gorm:"constraint:OnDelete:SET NULL;foreignKey:MaterialSetID;references:ID" json:"material_set,omitempty"`
	Title					string					`gorm:"column:title;not null" json:"title"`
	Description		string					`gorm:"column:description" json:"description"`
	Level					string					`gorm:"column:level" json:"level"`
	Subject				string					`gorm:"column:subject" json:"subject"`
	Metadata			datatypes.JSON	`gorm:"column:metadata;type:jsonb" json:"metadata"`
	Progress			int							`gorm:"column:progress;not null;default:0" json:"progress"`
	CreatedAt			time.Time				`gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt			time.Time				`gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt			gorm.DeletedAt	`gorm:"index" json:"deleted_at,omitempty"`
}

func (Course) TableName() string { return "course" }










