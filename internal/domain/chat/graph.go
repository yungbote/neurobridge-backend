package chat

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

type ChatEntity struct {
	ID     uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	UserID uuid.UUID `gorm:"type:uuid;not null;index" json:"user_id"`

	Scope   string     `gorm:"type:text;not null;index" json:"scope"` // thread|path|user
	ScopeID *uuid.UUID `gorm:"type:uuid;index" json:"scope_id,omitempty"`

	ThreadID *uuid.UUID `gorm:"type:uuid;index" json:"thread_id,omitempty"`
	PathID   *uuid.UUID `gorm:"type:uuid;index" json:"path_id,omitempty"`
	JobID    *uuid.UUID `gorm:"type:uuid;index" json:"job_id,omitempty"`

	Name        string         `gorm:"type:text;not null;index" json:"name"`
	Type        string         `gorm:"type:text;not null;default:'unknown'" json:"type"`
	Description string         `gorm:"type:text;not null;default:''" json:"description"`
	Aliases     datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'" json:"aliases"`

	UpdatedAt time.Time `gorm:"not null;default:now();index" json:"updated_at"`
}

func (ChatEntity) TableName() string { return "chat_entity" }

type ChatEdge struct {
	ID     uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	UserID uuid.UUID `gorm:"type:uuid;not null;index" json:"user_id"`

	Scope   string     `gorm:"type:text;not null;index" json:"scope"` // thread|path|user
	ScopeID *uuid.UUID `gorm:"type:uuid;index" json:"scope_id,omitempty"`

	SrcEntityID uuid.UUID `gorm:"type:uuid;not null;index" json:"src_entity_id"`
	DstEntityID uuid.UUID `gorm:"type:uuid;not null;index" json:"dst_entity_id"`

	Relation string  `gorm:"type:text;not null;index" json:"relation"`
	Weight   float64 `gorm:"not null;default:1.0" json:"weight"`

	EvidenceSeqs datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'" json:"evidence_seqs"`

	CreatedAt time.Time `gorm:"not null;default:now();index" json:"created_at"`
}

func (ChatEdge) TableName() string { return "chat_edge" }

type ChatClaim struct {
	ID     uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	UserID uuid.UUID `gorm:"type:uuid;not null;index" json:"user_id"`

	Scope   string     `gorm:"type:text;not null;index" json:"scope"` // thread|path|user
	ScopeID *uuid.UUID `gorm:"type:uuid;index" json:"scope_id,omitempty"`

	ThreadID *uuid.UUID `gorm:"type:uuid;index" json:"thread_id,omitempty"`
	PathID   *uuid.UUID `gorm:"type:uuid;index" json:"path_id,omitempty"`
	JobID    *uuid.UUID `gorm:"type:uuid;index" json:"job_id,omitempty"`

	Content string `gorm:"type:text;not null" json:"content"`

	EntityNames  datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'" json:"entity_names"`
	EvidenceSeqs datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'" json:"evidence_seqs"`

	CreatedAt time.Time `gorm:"not null;default:now();index" json:"created_at"`
}

func (ChatClaim) TableName() string { return "chat_claim" }
