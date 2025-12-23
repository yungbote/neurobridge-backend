package chat

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

// ChatDoc is a retrieval projection over canonical chat data.
// It is safe to rebuild from SQL truth (threads/messages + derived artifacts).
type ChatDoc struct {
	ID     uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	UserID uuid.UUID `gorm:"type:uuid;not null;index" json:"user_id"`

	DocType string `gorm:"type:text;not null;index" json:"doc_type"`

	Scope   string     `gorm:"type:text;not null;index" json:"scope"` // thread|path|user
	ScopeID *uuid.UUID `gorm:"type:uuid;index" json:"scope_id,omitempty"`

	ThreadID *uuid.UUID `gorm:"type:uuid;index" json:"thread_id,omitempty"`
	PathID   *uuid.UUID `gorm:"type:uuid;index" json:"path_id,omitempty"`
	JobID    *uuid.UUID `gorm:"type:uuid;index" json:"job_id,omitempty"`

	SourceID   *uuid.UUID `gorm:"type:uuid;index" json:"source_id,omitempty"`
	SourceSeq  *int64     `gorm:"index" json:"source_seq,omitempty"`
	ChunkIndex int        `gorm:"not null;default:0" json:"chunk_index"`

	Text           string `gorm:"type:text;not null" json:"text"`
	ContextualText string `gorm:"type:text;not null" json:"contextual_text"`

	Embedding datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'" json:"embedding"`
	VectorID  string         `gorm:"type:text;not null;index" json:"vector_id"`

	CreatedAt time.Time `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time `gorm:"not null;default:now();index" json:"updated_at"`
}

func (ChatDoc) TableName() string { return "chat_doc" }

