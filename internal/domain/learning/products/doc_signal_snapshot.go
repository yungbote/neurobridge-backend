package products

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

// UserDocSignalSnapshot is a versioned snapshot of signals used for doc generation.
type UserDocSignalSnapshot struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	UserID     uuid.UUID `gorm:"type:uuid;not null;index" json:"user_id"`
	PathID     uuid.UUID `gorm:"type:uuid;not null;index" json:"path_id"`
	PathNodeID uuid.UUID `gorm:"type:uuid;not null;index" json:"path_node_id"`

	SnapshotID    string `gorm:"column:snapshot_id;type:text;not null;uniqueIndex" json:"snapshot_id"`
	PolicyVersion string `gorm:"column:policy_version;type:text;not null;index" json:"policy_version"`
	SchemaVersion int    `gorm:"column:schema_version;not null" json:"schema_version"`

	SnapshotJSON datatypes.JSON `gorm:"type:jsonb;column:snapshot_json;not null" json:"snapshot_json"`

	CreatedAt time.Time `gorm:"not null;default:now();index" json:"created_at"`
}

func (UserDocSignalSnapshot) TableName() string { return "user_doc_signal_snapshot" }
