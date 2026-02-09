package personalization

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

// UserBeliefSnapshot stores a latent learner-state snapshot derived at runtime.
type UserBeliefSnapshot struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	UserID     uuid.UUID `gorm:"type:uuid;not null;index:idx_user_belief_snapshot,priority:1" json:"user_id"`
	PathID     uuid.UUID `gorm:"type:uuid;not null;index:idx_user_belief_snapshot,priority:2" json:"path_id"`
	PathNodeID uuid.UUID `gorm:"type:uuid;not null;index:idx_user_belief_snapshot,priority:3" json:"path_node_id"`

	SnapshotID    string `gorm:"column:snapshot_id;type:text;not null;uniqueIndex" json:"snapshot_id"`
	PolicyVersion string `gorm:"column:policy_version;type:text;not null;index" json:"policy_version"`
	SchemaVersion int    `gorm:"column:schema_version;not null" json:"schema_version"`

	SessionID string `gorm:"column:session_id;type:text;index" json:"session_id,omitempty"`

	SnapshotJSON datatypes.JSON `gorm:"type:jsonb;column:snapshot_json;not null" json:"snapshot_json"`

	CreatedAt time.Time `gorm:"not null;default:now();index" json:"created_at"`
}

func (UserBeliefSnapshot) TableName() string { return "user_belief_snapshot" }
