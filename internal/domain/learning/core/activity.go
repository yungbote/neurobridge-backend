package core

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type Activity struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	OwnerType string     `gorm:"column:owner_type;index" json:"owner_type,omitempty"` // "path"
	OwnerID   *uuid.UUID `gorm:"type:uuid;column:owner_id;index" json:"owner_id,omitempty"`

	Kind  string `gorm:"column:kind;not null;default:'reading';index" json:"kind"`
	Title string `gorm:"column:title;not null" json:"title"`

	ContentMD        string         `gorm:"column:content_md;type:text" json:"content_md,omitempty"`
	ContentJSON      datatypes.JSON `gorm:"column:content_json;type:jsonb" json:"content_json,omitempty"`
	EstimatedMinutes int            `gorm:"column:estimated_minutes;not null;default:10" json:"estimated_minutes"`
	Difficulty       string         `gorm:"column:difficulty;index" json:"difficulty,omitempty"`
	Status           string         `gorm:"column:status;not null;default:'draft';index" json:"status"`
	Metadata         datatypes.JSON `gorm:"column:metadata;type:jsonb" json:"metadata,omitempty"`

	CreatedAt time.Time      `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (Activity) TableName() string { return "activity" }
