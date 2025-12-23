package chat

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type ChatMessage struct {
	ID       uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	ThreadID uuid.UUID `gorm:"type:uuid;not null;index;index:idx_chat_message_thread_seq,unique,priority:1" json:"thread_id"`
	UserID   uuid.UUID `gorm:"type:uuid;not null;index" json:"user_id"`

	Seq int64 `gorm:"column:seq;not null;index:idx_chat_message_thread_seq,unique,priority:2,index" json:"seq"`

	Role   string `gorm:"column:role;not null;index" json:"role"`
	Status string `gorm:"column:status;not null;default:'sent';index" json:"status"`

	Content  string         `gorm:"column:content;type:text;not null;default:''" json:"content"`
	Model    string         `gorm:"column:model" json:"model,omitempty"`
	Metadata datatypes.JSON `gorm:"type:jsonb;column:metadata;not null;default:'{}'" json:"metadata,omitempty"`

	// Optional: client-provided idempotency key to dedupe retries for user messages.
	// Enforced via a partial unique index (role='user' AND idempotency_key <> '').
	IdempotencyKey string `gorm:"type:text;column:idempotency_key;not null;default:'';index" json:"idempotency_key,omitempty"`

	CreatedAt time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (ChatMessage) TableName() string { return "chat_message" }








