package personalization

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// MisconceptionCausalEdge captures user-specific causal links between misconceptions.
type MisconceptionCausalEdge struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	UserID        uuid.UUID      `gorm:"type:uuid;not null;index:idx_miscon_causal_edge,priority:1;uniqueIndex:idx_miscon_causal_edge_key,priority:1" json:"user_id"`
	FromConceptID uuid.UUID      `gorm:"type:uuid;not null;index:idx_miscon_causal_edge,priority:2;uniqueIndex:idx_miscon_causal_edge_key,priority:2" json:"from_concept_id"`
	ToConceptID   uuid.UUID      `gorm:"type:uuid;not null;index:idx_miscon_causal_edge,priority:3;uniqueIndex:idx_miscon_causal_edge_key,priority:3" json:"to_concept_id"`
	EdgeType      string         `gorm:"column:edge_type;type:text;not null;index:idx_miscon_causal_edge,priority:4;uniqueIndex:idx_miscon_causal_edge_key,priority:4" json:"edge_type"`
	Strength      float64        `gorm:"column:strength;not null;default:0" json:"strength"`
	Count         int            `gorm:"column:count;not null;default:0" json:"count"`
	SchemaVersion int            `gorm:"column:schema_version;not null;default:1" json:"schema_version"`
	Evidence      datatypes.JSON `gorm:"column:evidence;type:jsonb" json:"evidence,omitempty"`
	LastSeenAt    *time.Time     `gorm:"column:last_seen_at;index" json:"last_seen_at,omitempty"`

	CreatedAt time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (MisconceptionCausalEdge) TableName() string { return "misconception_causal_edge" }
