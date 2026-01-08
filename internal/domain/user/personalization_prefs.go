package user

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// UserPersonalizationPrefs stores user-controlled learning preferences that should persist across devices.
// This includes teaching-style defaults and accessibility needs used during generation.
type UserPersonalizationPrefs struct {
	ID     uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	UserID uuid.UUID `gorm:"type:uuid;not null;uniqueIndex" json:"user_id"`

	PrefsJSON datatypes.JSON `gorm:"column:prefs_json;type:jsonb;not null;default:'{}'" json:"prefs_json"`

	UpdatedAt time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (UserPersonalizationPrefs) TableName() string { return "user_personalization_prefs" }

