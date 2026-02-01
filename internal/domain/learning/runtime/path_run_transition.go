package runtime

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// PathRunTransition is an append-only log of runtime transitions.
type PathRunTransition struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	UserID  uuid.UUID `gorm:"type:uuid;not null;index:idx_path_run_transition_user_event,unique,priority:1" json:"user_id"`
	EventID uuid.UUID `gorm:"type:uuid;not null;index:idx_path_run_transition_user_event,unique,priority:2;index" json:"event_id"`

	PathID    uuid.UUID `gorm:"type:uuid;not null;index" json:"path_id"`
	EventType string    `gorm:"column:event_type;type:text;not null;index" json:"event_type"`

	FromState string `gorm:"column:from_state;type:text" json:"from_state,omitempty"`
	ToState   string `gorm:"column:to_state;type:text" json:"to_state,omitempty"`

	OccurredAt time.Time      `gorm:"column:occurred_at;not null;index" json:"occurred_at"`
	Payload    datatypes.JSON `gorm:"column:payload;type:jsonb" json:"payload,omitempty"`

	CreatedAt time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (PathRunTransition) TableName() string { return "path_run_transition" }
