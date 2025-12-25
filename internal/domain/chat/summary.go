package chat

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

// ChatSummaryNode stores a RAPTOR-style hierarchical summary tree for a thread.
type ChatSummaryNode struct {
	ID       uuid.UUID  `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	ThreadID uuid.UUID  `gorm:"type:uuid;not null;index" json:"thread_id"`
	ParentID *uuid.UUID `gorm:"type:uuid;index" json:"parent_id,omitempty"`

	Level    int   `gorm:"not null;index" json:"level"`
	StartSeq int64 `gorm:"not null;index" json:"start_seq"`
	EndSeq   int64 `gorm:"not null;index" json:"end_seq"`

	SummaryMD    string         `gorm:"type:text;not null" json:"summary_md"`
	ChildNodeIDs datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'" json:"child_node_ids"`

	CreatedAt time.Time `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time `gorm:"not null;default:now();index" json:"updated_at"`
}

func (ChatSummaryNode) TableName() string { return "chat_summary_node" }
