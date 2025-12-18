package learning

import (
	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"time"
)

type ActivityVariant struct {
	ID          uuid.UUID      `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	ActivityID  uuid.UUID      `gorm:"type:uuid;not null;index:idx_activity_variant,unique,priority:1" json:"activity_id"`
	Activity    *Activity      `gorm:"constraint:OnDelete:CASCADE;foreignKey:ActivityID;references:ID" json:"activity,omitempty"`
	Variant     string         `gorm:"column:variant;not null;index:idx_activity_variant,unique,priority:2" json:"variant"` // concise|full|diagram_heavy|examples_first
	ContentMD   string         `gorm:"column:content_md;type:text" json:"content_md,omitempty"`
	ContentJSON datatypes.JSON `gorm:"column:content_json;type:jsonb" json:"content_json,omitempty"`
	// RenderSpec is where “diagram blocks / image blocks / typography hints” go.
	RenderSpec datatypes.JSON `gorm:"column:render_spec;type:jsonb" json:"render_spec,omitempty"`
	CreatedAt  time.Time      `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt  time.Time      `gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt  gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (ActivityVariant) TableName() string { return "activity_variant" }
