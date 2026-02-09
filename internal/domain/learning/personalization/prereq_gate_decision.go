package personalization

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

// PrereqGateDecision records the gating decision taken at node entry.
type PrereqGateDecision struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	UserID     uuid.UUID `gorm:"type:uuid;not null;index:idx_prereq_gate_decision,priority:1;uniqueIndex:idx_prereq_gate_identity,priority:1" json:"user_id"`
	PathID     uuid.UUID `gorm:"type:uuid;not null;index:idx_prereq_gate_decision,priority:2" json:"path_id"`
	PathNodeID uuid.UUID `gorm:"type:uuid;not null;index:idx_prereq_gate_decision,priority:3;uniqueIndex:idx_prereq_gate_identity,priority:2" json:"path_node_id"`

	SnapshotID string `gorm:"column:snapshot_id;type:text;not null;index:idx_prereq_gate_decision,priority:4;uniqueIndex:idx_prereq_gate_identity,priority:3" json:"snapshot_id"`

	PolicyVersion string `gorm:"column:policy_version;type:text;not null;index" json:"policy_version"`
	SchemaVersion int    `gorm:"column:schema_version;not null" json:"schema_version"`

	ReadinessStatus string  `gorm:"column:readiness_status;type:text;not null;index" json:"readiness_status"`
	ReadinessScore  float64 `gorm:"column:readiness_score;not null;default:0" json:"readiness_score"`

	GateMode string `gorm:"column:gate_mode;type:text;not null;index" json:"gate_mode"`
	Decision string `gorm:"column:decision;type:text;not null;index" json:"decision"`
	Reason   string `gorm:"column:reason;type:text;not null" json:"reason"`

	EvidenceJSON datatypes.JSON `gorm:"type:jsonb;column:evidence_json" json:"evidence_json,omitempty"`

	CreatedAt time.Time `gorm:"not null;default:now();index" json:"created_at"`
}

func (PrereqGateDecision) TableName() string { return "prereq_gate_decision" }
