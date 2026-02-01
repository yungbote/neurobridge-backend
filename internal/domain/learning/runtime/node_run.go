package runtime

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type NodeRunState string

const (
	NodeRunNotStarted NodeRunState = "not_started"
	NodeRunReading    NodeRunState = "reading"
	NodeRunPractice   NodeRunState = "practice"
	NodeRunCompleted  NodeRunState = "completed"
)

// NodeRun represents per-user progress on a path node.
type NodeRun struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	UserID uuid.UUID `gorm:"type:uuid;not null;index:idx_node_run_user_node,priority:1" json:"user_id"`
	PathID uuid.UUID `gorm:"type:uuid;not null;index" json:"path_id"`
	NodeID uuid.UUID `gorm:"type:uuid;not null;index:idx_node_run_user_node,priority:2;index" json:"node_id"`

	State NodeRunState `gorm:"column:state;type:text;not null;default:'not_started'" json:"state"`

	StartedAt   *time.Time `gorm:"column:started_at;index" json:"started_at,omitempty"`
	CompletedAt *time.Time `gorm:"column:completed_at;index" json:"completed_at,omitempty"`
	LastSeenAt  *time.Time `gorm:"column:last_seen_at;index" json:"last_seen_at,omitempty"`

	AttemptCount int     `gorm:"column:attempt_count;not null;default:0" json:"attempt_count"`
	LastScore    float64 `gorm:"column:last_score;not null;default:0" json:"last_score"`

	Metadata datatypes.JSON `gorm:"column:metadata;type:jsonb" json:"metadata,omitempty"`

	CreatedAt time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (NodeRun) TableName() string { return "node_run" }
