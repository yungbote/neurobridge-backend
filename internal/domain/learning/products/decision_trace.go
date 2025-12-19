package products

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// Policy decision trace.
type DecisionTrace struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	UserID     uuid.UUID `gorm:"type:uuid;not null;index:idx_decision_time,priority:1" json:"user_id"`
	OccurredAt time.Time `gorm:"column:occurred_at;not null;index:idx_decision_time,priority:2" json:"occurred_at"`

	DecisionType string     `gorm:"column:decision_type;not null;index" json:"decision_type"`
	CourseID     *uuid.UUID `gorm:"type:uuid;index" json:"course_id,omitempty"`
	PathID       *uuid.UUID `gorm:"type:uuid;index" json:"path_id,omitempty"`
	ActivityID   *uuid.UUID `gorm:"type:uuid;index" json:"activity_id,omitempty"`
	VariantID    *uuid.UUID `gorm:"type:uuid;index" json:"variant_id,omitempty"`

	Inputs     datatypes.JSON `gorm:"column:inputs;type:jsonb" json:"inputs"`
	Candidates datatypes.JSON `gorm:"column:candidates;type:jsonb" json:"candidates"`
	Chosen     datatypes.JSON `gorm:"column:chosen;type:jsonb" json:"chosen"`

	CreatedAt time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (DecisionTrace) TableName() string { return "decision_trace" }
