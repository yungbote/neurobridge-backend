package personalization

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

// ConceptReadinessSnapshot captures prereq readiness at node entry.
type ConceptReadinessSnapshot struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	UserID     uuid.UUID `gorm:"type:uuid;not null;index" json:"user_id"`
	PathID     uuid.UUID `gorm:"type:uuid;not null;index" json:"path_id"`
	PathNodeID uuid.UUID `gorm:"type:uuid;not null;index" json:"path_node_id"`

	SnapshotID    string `gorm:"column:snapshot_id;type:text;not null;uniqueIndex" json:"snapshot_id"`
	PolicyVersion string `gorm:"column:policy_version;type:text;not null;index" json:"policy_version"`
	SchemaVersion int    `gorm:"column:schema_version;not null" json:"schema_version"`

	Status string  `gorm:"column:status;type:text;not null;index" json:"status"`
	Score  float64 `gorm:"column:score;not null;default:0" json:"score"`

	SnapshotJSON datatypes.JSON `gorm:"type:jsonb;column:snapshot_json;not null" json:"snapshot_json"`

	CreatedAt time.Time `gorm:"not null;default:now();index" json:"created_at"`
}

func (ConceptReadinessSnapshot) TableName() string { return "concept_readiness_snapshot" }
