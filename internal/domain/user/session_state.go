package user

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

// UserSessionState stores lightweight runtime state keyed by a session (UserToken.ID).
// This is intended for "resume" and cross-request continuity, not for analytics (use user_event for that).
type UserSessionState struct {
	// SessionID is the current session identifier (UserToken.ID).
	SessionID uuid.UUID `gorm:"type:uuid;primaryKey" json:"session_id"`
	UserID    uuid.UUID `gorm:"type:uuid;not null;index" json:"user_id"`

	// Active context pointers (nullable).
	ActivePathID       *uuid.UUID `gorm:"type:uuid;column:active_path_id;index" json:"active_path_id,omitempty"`
	ActivePathNodeID   *uuid.UUID `gorm:"type:uuid;column:active_path_node_id;index" json:"active_path_node_id,omitempty"`
	ActiveActivityID   *uuid.UUID `gorm:"type:uuid;column:active_activity_id;index" json:"active_activity_id,omitempty"`
	ActiveChatThreadID *uuid.UUID `gorm:"type:uuid;column:active_chat_thread_id;index" json:"active_chat_thread_id,omitempty"`
	ActiveJobID        *uuid.UUID `gorm:"type:uuid;column:active_job_id;index" json:"active_job_id,omitempty"`

	// Lightweight UI/resume hints (nullable).
	ActiveRoute      *string  `gorm:"column:active_route;type:text" json:"active_route,omitempty"`
	ActiveView       *string  `gorm:"column:active_view;type:text" json:"active_view,omitempty"`
	ActiveDocBlockID *string  `gorm:"column:active_doc_block_id;type:text" json:"active_doc_block_id,omitempty"`
	ScrollPercent    *float64 `gorm:"column:scroll_percent" json:"scroll_percent,omitempty"`

	// Extensible metadata for future runtime fields.
	Metadata datatypes.JSON `gorm:"column:metadata;type:jsonb" json:"metadata,omitempty"`

	LastSeenAt time.Time `gorm:"column:last_seen_at;not null;default:now();index" json:"last_seen_at"`
	CreatedAt  time.Time `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt  time.Time `gorm:"not null;default:now();index" json:"updated_at"`
}

func (UserSessionState) TableName() string { return "user_session_state" }
