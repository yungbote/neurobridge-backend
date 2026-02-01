package runtime

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type ActivityRunState string

const (
	ActivityRunNotStarted ActivityRunState = "not_started"
	ActivityRunAttempting ActivityRunState = "attempting"
	ActivityRunEvaluated  ActivityRunState = "evaluated"
	ActivityRunCompleted  ActivityRunState = "completed"
)

// ActivityRun represents per-user progress on a specific activity.
type ActivityRun struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	UserID     uuid.UUID `gorm:"type:uuid;not null;index:idx_activity_run_user_activity,priority:1" json:"user_id"`
	PathID     uuid.UUID `gorm:"type:uuid;not null;index" json:"path_id"`
	NodeID     uuid.UUID `gorm:"type:uuid;column:node_id;index" json:"node_id"`
	ActivityID uuid.UUID `gorm:"type:uuid;not null;index:idx_activity_run_user_activity,priority:2;index" json:"activity_id"`

	State ActivityRunState `gorm:"column:state;type:text;not null;default:'not_started'" json:"state"`

	Attempts      int        `gorm:"column:attempts;not null;default:0" json:"attempts"`
	LastScore     float64    `gorm:"column:last_score;not null;default:0" json:"last_score"`
	LastAttemptAt *time.Time `gorm:"column:last_attempt_at;index" json:"last_attempt_at,omitempty"`
	CompletedAt   *time.Time `gorm:"column:completed_at;index" json:"completed_at,omitempty"`

	Metadata datatypes.JSON `gorm:"column:metadata;type:jsonb" json:"metadata,omitempty"`

	CreatedAt time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (ActivityRun) TableName() string { return "activity_run" }
