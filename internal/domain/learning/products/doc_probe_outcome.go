package products

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

// DocProbeOutcome captures completion/dismissal outcomes for probes.
type DocProbeOutcome struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	ProbeID uuid.UUID `gorm:"type:uuid;not null;index" json:"probe_id"`

	UserID     uuid.UUID `gorm:"type:uuid;not null;index" json:"user_id"`
	PathID     uuid.UUID `gorm:"type:uuid;not null;index" json:"path_id"`
	PathNodeID uuid.UUID `gorm:"type:uuid;not null;index" json:"path_node_id"`

	BlockID string `gorm:"column:block_id;type:text;not null;index" json:"block_id"`

	EventID   *uuid.UUID `gorm:"type:uuid;column:event_id;index" json:"event_id,omitempty"`
	EventType string     `gorm:"column:event_type;type:text;not null;index" json:"event_type"`
	Outcome   string     `gorm:"column:outcome;type:text;not null;index" json:"outcome"`

	IsCorrect  *bool   `gorm:"column:is_correct" json:"is_correct,omitempty"`
	LatencyMS  int     `gorm:"column:latency_ms;not null;default:0" json:"latency_ms"`
	Confidence float64 `gorm:"column:confidence;not null;default:0" json:"confidence"`

	Payload datatypes.JSON `gorm:"type:jsonb;column:payload" json:"payload,omitempty"`

	CreatedAt time.Time `gorm:"not null;default:now();index" json:"created_at"`
}

func (DocProbeOutcome) TableName() string { return "doc_probe_outcome" }
