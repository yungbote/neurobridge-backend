package personalization

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// Compact progression facts (derived from UserEvent).
type UserProgressionEvent struct {
	ID         uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	UserID     uuid.UUID `gorm:"type:uuid;not null;index:idx_user_prog_time,priority:1" json:"user_id"`
	OccurredAt time.Time `gorm:"column:occurred_at;not null;index:idx_user_prog_time,priority:2" json:"occurred_at"`

	CourseID   *uuid.UUID `gorm:"type:uuid;index" json:"course_id,omitempty"`
	PathID     *uuid.UUID `gorm:"type:uuid;index" json:"path_id,omitempty"`
	ActivityID *uuid.UUID `gorm:"type:uuid;index" json:"activity_id,omitempty"`

	ConceptIDs datatypes.JSON `gorm:"column:concept_ids;type:jsonb" json:"concept_ids"`

	ActivityKind string `gorm:"column:activity_kind;index" json:"activity_kind,omitempty"`
	Variant      string `gorm:"column:variant;index" json:"variant,omitempty"`

	Completed bool    `gorm:"column:completed;not null;default:false;index" json:"completed"`
	Score     float64 `gorm:"column:score;not null;default:0" json:"score"`
	DwellMS   int     `gorm:"column:dwell_ms;not null;default:0" json:"dwell_ms"`
	Attempts  int     `gorm:"column:attempts;not null;default:0" json:"attempts"`

	Metadata datatypes.JSON `gorm:"column:metadata;type:jsonb" json:"metadata"`

	CreatedAt time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (UserProgressionEvent) TableName() string { return "user_progression_event" }
