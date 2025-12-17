package types

import (
	"time"
	"github.com/google/uuid"
	"gorm.io/gorm"
)


type UserStylePreference struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	UserID    uuid.UUID  `gorm:"type:uuid;not null;index:idx_user_style_pref,unique,priority:1" json:"user_id"`
	ConceptID *uuid.UUID `gorm:"type:uuid;index:idx_user_style_pref,unique,priority:2" json:"concept_id,omitempty"` // nil => global user pref
	Modality string `gorm:"column:modality;not null;index:idx_user_style_pref,unique,priority:3" json:"modality"` // e.g. "diagram"|"text"|"analogy"
	Variant  string `gorm:"column:variant;not null;index:idx_user_style_pref,unique,priority:4" json:"variant"`   // e.g. "flowchart"|"table"|"timeline"|"dense"|"spacious"
	// EMA reward in [-1..+1] typically
	EMA float64 `gorm:"column:ema;not null;default:0" json:"ema"`
	N   int     `gorm:"column:n;not null;default:0" json:"n"`
	// Optional bandit stats (Beta) for binary rewards
	A float64 `gorm:"column:a;not null;default:1" json:"a"`
	B float64 `gorm:"column:b;not null;default:1" json:"b"`
	UpdatedAt time.Time      `gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (UserStylePreference) TableName() string { return "user_style_preference" }










