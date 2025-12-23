package chat

import (
	"time"

	"github.com/google/uuid"
)

// ChatThreadState stores incremental maintenance cursors for a thread.
// It is updated by background jobs (indexing, summarization, graph, memory).
type ChatThreadState struct {
	ThreadID uuid.UUID `gorm:"type:uuid;primaryKey" json:"thread_id"`

	LastIndexedSeq    int64 `gorm:"column:last_indexed_seq;not null;default:0" json:"last_indexed_seq"`
	LastSummarizedSeq int64 `gorm:"column:last_summarized_seq;not null;default:0" json:"last_summarized_seq"`
	LastGraphSeq      int64 `gorm:"column:last_graph_seq;not null;default:0" json:"last_graph_seq"`
	LastMemorySeq     int64 `gorm:"column:last_memory_seq;not null;default:0" json:"last_memory_seq"`

	OpenAIConversationID *string `gorm:"column:openai_conversation_id;type:text" json:"openai_conversation_id,omitempty"`

	UpdatedAt time.Time `gorm:"not null;default:now();index" json:"updated_at"`
}

func (ChatThreadState) TableName() string { return "chat_thread_state" }
