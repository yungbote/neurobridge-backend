package chat

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// ChatTurn ties together a single user message -> assistant message generation.
// It is the canonical per-turn trace anchor: request -> retrieval -> streaming -> maintenance.
type ChatTurn struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	UserID   uuid.UUID `gorm:"type:uuid;not null;index" json:"user_id"`
	ThreadID uuid.UUID `gorm:"type:uuid;not null;index" json:"thread_id"`

	UserMessageID      uuid.UUID `gorm:"type:uuid;not null;index" json:"user_message_id"`
	AssistantMessageID uuid.UUID `gorm:"type:uuid;not null;index" json:"assistant_message_id"`

	JobID *uuid.UUID `gorm:"type:uuid;index" json:"job_id,omitempty"`

	Status  string `gorm:"type:text;not null;default:'queued';index" json:"status"`
	Attempt int    `gorm:"not null;default:0" json:"attempt"`

	OpenAIConversationID *string        `gorm:"type:text" json:"openai_conversation_id,omitempty"`
	RetrievalTrace       datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'" json:"retrieval_trace"`

	StartedAt   *time.Time `gorm:"index" json:"started_at,omitempty"`
	CompletedAt *time.Time `gorm:"index" json:"completed_at,omitempty"`

	CreatedAt time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (ChatTurn) TableName() string { return "chat_turn" }

