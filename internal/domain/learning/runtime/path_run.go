package runtime

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type PathRunState string

const (
	PathRunNotStarted      PathRunState = "not_started"
	PathRunInNode          PathRunState = "in_node"
	PathRunInActivity      PathRunState = "in_activity"
	PathRunAwaitingFeed    PathRunState = "awaiting_feedback"
	PathRunAwaitingUser    PathRunState = "awaiting_user"
	PathRunReviewScheduled PathRunState = "review_scheduled"
	PathRunPaused          PathRunState = "paused"
	PathRunCompleted       PathRunState = "completed"
)

// PathRun represents per-user runtime progress for a path.
type PathRun struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	UserID uuid.UUID `gorm:"type:uuid;not null;index:idx_path_run_user_path,priority:1" json:"user_id"`
	PathID uuid.UUID `gorm:"type:uuid;not null;index:idx_path_run_user_path,priority:2;index" json:"path_id"`

	State PathRunState `gorm:"column:state;type:text;not null;default:'not_started'" json:"state"`

	ActiveNodeID     *uuid.UUID `gorm:"type:uuid;column:active_node_id;index" json:"active_node_id,omitempty"`
	ActiveActivityID *uuid.UUID `gorm:"type:uuid;column:active_activity_id;index" json:"active_activity_id,omitempty"`

	Strategy datatypes.JSON `gorm:"column:strategy;type:jsonb" json:"strategy,omitempty"`
	Metadata datatypes.JSON `gorm:"column:metadata;type:jsonb" json:"metadata,omitempty"`

	LastEventID *uuid.UUID `gorm:"type:uuid;column:last_event_id;index" json:"last_event_id,omitempty"`
	LastEventAt *time.Time `gorm:"column:last_event_at;index" json:"last_event_at,omitempty"`

	CreatedAt time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (PathRun) TableName() string { return "path_run" }
