package learning

import (
	"time"

	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/domain/user"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type LearningProfile struct {
	ID     uuid.UUID  `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	UserID uuid.UUID  `gorm:"type:uuid;not null;uniqueIndex" json:"user_id"`
	User   *user.User `gorm:"constraint:OnDelete:CASCADE;foreignKey:UserID;references:ID" json:"user,omitempty"`

	Diagnoses     datatypes.JSON `gorm:"type:jsonb;column:diagnoses" json:"diagnoses"`
	Accomodations datatypes.JSON `gorm:"type:jsonb;column:accommodations" json:"accomodations"`
	Constraints   datatypes.JSON `gorm:"type:jsonb;column:constraints" json:"constraints"`
	Preferences   datatypes.JSON `gorm:"type:jsonb;column:preferences" json:"preferences"`
	Notes         string         `gorm:"column:notes" json:"notes"`

	CreatedAt time.Time      `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (LearningProfile) TableName() string { return "learning_profile" }
