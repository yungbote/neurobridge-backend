package personalization

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// UserGazeEvent stores raw gaze hits (optional; use only with explicit opt-in).
type UserGazeEvent struct {
	ID         uuid.UUID      `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	UserID     uuid.UUID      `gorm:"type:uuid;not null;index:idx_user_gaze_event_user,priority:1" json:"user_id"`
	SessionID  uuid.UUID      `gorm:"type:uuid;not null;index:idx_user_gaze_event_session,priority:2" json:"session_id"`
	PathID     *uuid.UUID     `gorm:"type:uuid;column:path_id;index" json:"path_id,omitempty"`
	PathNodeID *uuid.UUID     `gorm:"type:uuid;column:path_node_id;index" json:"path_node_id,omitempty"`
	BlockID    string         `gorm:"column:block_id;index" json:"block_id,omitempty"`
	LineID     string         `gorm:"column:line_id;index" json:"line_id,omitempty"`
	X          float64        `gorm:"column:x" json:"x"`
	Y          float64        `gorm:"column:y" json:"y"`
	Confidence float64        `gorm:"column:confidence" json:"confidence"`
	OccurredAt time.Time      `gorm:"column:occurred_at;index" json:"occurred_at"`
	Metadata   datatypes.JSON `gorm:"type:jsonb;column:metadata" json:"metadata,omitempty"`
	CreatedAt  time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt  time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt  gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (UserGazeEvent) TableName() string { return "user_gaze_event" }

// UserGazeBlockStat stores aggregated gaze signals per block.
type UserGazeBlockStat struct {
	ID            uuid.UUID      `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	UserID        uuid.UUID      `gorm:"type:uuid;not null;index:idx_user_gaze_block,unique,priority:1" json:"user_id"`
	SessionID     uuid.UUID      `gorm:"type:uuid;not null;index:idx_user_gaze_block,unique,priority:2" json:"session_id"`
	PathID        *uuid.UUID     `gorm:"type:uuid;column:path_id;index" json:"path_id,omitempty"`
	PathNodeID    *uuid.UUID     `gorm:"type:uuid;column:path_node_id;index" json:"path_node_id,omitempty"`
	BlockID       string         `gorm:"column:block_id;not null;index:idx_user_gaze_block,unique,priority:3" json:"block_id"`
	FixationMs    int            `gorm:"column:fixation_ms" json:"fixation_ms"`
	FixationCount int            `gorm:"column:fixation_count" json:"fixation_count"`
	ReadCredit    float64        `gorm:"column:read_credit" json:"read_credit"`
	LastSeenAt    *time.Time     `gorm:"column:last_seen_at;index" json:"last_seen_at,omitempty"`
	Metadata      datatypes.JSON `gorm:"type:jsonb;column:metadata" json:"metadata,omitempty"`
	CreatedAt     time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt     time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt     gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (UserGazeBlockStat) TableName() string { return "user_gaze_block_stat" }
