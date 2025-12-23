package chat

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type ChatMemoryItem struct {
	ID     uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	UserID uuid.UUID `gorm:"type:uuid;not null;index" json:"user_id"`

	Scope   string     `gorm:"type:text;not null;index" json:"scope"` // thread|path|user
	ScopeID *uuid.UUID `gorm:"type:uuid;index" json:"scope_id,omitempty"`

	ThreadID *uuid.UUID `gorm:"type:uuid;index" json:"thread_id,omitempty"`
	PathID   *uuid.UUID `gorm:"type:uuid;index" json:"path_id,omitempty"`
	JobID    *uuid.UUID `gorm:"type:uuid;index" json:"job_id,omitempty"`

	Kind       string  `gorm:"type:text;not null;index" json:"kind"` // fact|preference|decision|todo
	Key        string  `gorm:"type:text;not null" json:"key"`
	Value      string  `gorm:"type:text;not null" json:"value"`
	Confidence float64 `gorm:"not null;default:0.0" json:"confidence"`

	EvidenceSeqs datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'" json:"evidence_seqs"`

	CreatedAt time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (ChatMemoryItem) TableName() string { return "chat_memory_item" }

