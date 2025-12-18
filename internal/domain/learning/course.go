package learning

import (
	"time"

	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/domain/materials"
	"github.com/yungbote/neurobridge-backend/internal/domain/user"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type Course struct {
	ID            uuid.UUID              `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	UserID        uuid.UUID              `gorm:"type:uuid;not null;index" json:"user_id"`
	User          *user.User             `gorm:"constraint:OnDelete:CASCADE;foreignKey:UserID;references:ID" json:"user,omitempty"`
	MaterialSetID *uuid.UUID             `gorm:"type:uuid;index" json:"material_set_id,omitempty"`
	MaterialSet   *materials.MaterialSet `gorm:"constraint:OnDelete:SET NULL;foreignKey:MaterialSetID;references:ID" json:"material_set,omitempty"`

	Title       string `gorm:"column:title;not null" json:"title"`
	Description string `gorm:"column:description" json:"description"`
	Level       string `gorm:"column:level" json:"level"`
	Subject     string `gorm:"column:subject" json:"subject"`

	// NEW: core lifecycle + expanded metadata as real columns
	Status          string     `gorm:"column:status;not null;default:'generating';index" json:"status"`
	LongTitle       string     `gorm:"column:long_title" json:"long_title,omitempty"`
	LongDescription string     `gorm:"column:long_description;type:text" json:"long_description,omitempty"`
	GeneratedAt     *time.Time `gorm:"column:generated_at;index" json:"generated_at,omitempty"`
	FailedAt        *time.Time `gorm:"column:failed_at;index" json:"failed_at,omitempty"`
	ErrorMessage    string     `gorm:"column:error_message;type:text" json:"error_message,omitempty"`

	// Keep for truly optional / legacy keys (we keep metadata.status dual-write for now)
	Metadata  datatypes.JSON `gorm:"column:metadata;type:jsonb" json:"metadata"`
	Progress  int            `gorm:"column:progress;not null;default:0" json:"progress"`
	CreatedAt time.Time      `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (Course) TableName() string { return "course" }
