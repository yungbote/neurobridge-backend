package legacy_course

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type Lesson struct {
	ID       uuid.UUID     `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	ModuleID uuid.UUID     `gorm:"type:uuid;not null;index" json:"module_id"`
	Module   *CourseModule `gorm:"constraint:OnDelete:CASCADE;foreignKey:ModuleID;references:ID" json:"module,omitempty"`
	Index    int           `gorm:"column:index;not null" json:"index"`
	Title    string        `gorm:"column:title;not null" json:"title"`
	Kind     string        `gorm:"column:kind;not null;default:'reading'" json:"kind"`

	ContentMD string `gorm:"column:content_md;type:text" json:"content_md"`
	SummaryMD string `gorm:"column:summary_md;type:text" json:"summary_md"`

	ContentJSON      datatypes.JSON `gorm:"column:content_json;type:jsonb" json:"content_json"`
	EstimatedMinutes int            `gorm:"column:estimated_minutes" json:"estimated_minutes"`
	Metadata         datatypes.JSON `gorm:"column:metadata;type:jsonb" json:"metadata"`

	CreatedAt time.Time      `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (Lesson) TableName() string { return "lesson" }
