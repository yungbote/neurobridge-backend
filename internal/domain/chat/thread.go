package chat

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type ChatThread struct {
	ID     uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	UserID uuid.UUID `gorm:"type:uuid;not null;index" json:"user_id"`

	PathID *uuid.UUID `gorm:"type:uuid;column:path_id;index" json:"path_id,omitempty"`
	JobID  *uuid.UUID `gorm:"type:uuid;column:job_id;index" json:"job_id,omitempty"`

	Title    string `gorm:"column:title;not null;default:'New Chat'" json:"title"`
	Status   string `gorm:"column:status;not null;default:'active';index" json:"status"`
	Metadata datatypes.JSON `gorm:"type:jsonb;column:metadata;not null;default:'{}'" json:"metadata,omitempty"`

	// Concurrency-safe per-thread sequencing:
	NextSeq int64 `gorm:"column:next_seq;not null;default:0" json:"next_seq"`

	LastMessageAt time.Time `gorm:"column:last_message_at;not null;default:now();index" json:"last_message_at"`
	LastViewedAt  time.Time `gorm:"column:last_viewed_at;not null;default:now();index" json:"last_viewed_at"`

	CreatedAt time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (ChatThread) TableName() string { return "chat_thread" }









