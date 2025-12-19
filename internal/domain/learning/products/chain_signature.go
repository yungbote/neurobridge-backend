package learning

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// ChainSignature is a canonical, reusable identity for a concept chain/subgraph.
// It supports both deterministic keys (chain_key) and similarity search (embedding).
type ChainSignature struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	// Deterministic identity (stable across users when the same chain is defined).
	ChainKey string `gorm:"column:chain_key;not null;uniqueIndex" json:"chain_key"`

	// Scope allows you to store chain signatures per-path or global.
	// - scope="path", scope_id=<path_id>
	// - scope="global", scope_id=NULL
	Scope   string     `gorm:"column:scope;not null;default:'path';index:idx_chain_sig_scope" json:"scope"`
	ScopeID *uuid.UUID `gorm:"type:uuid;column:scope_id;index:idx_chain_sig_scope" json:"scope_id,omitempty"`

	// Canonical payload
	ConceptKeys datatypes.JSON `gorm:"column:concept_keys;type:jsonb" json:"concept_keys"` // []string (snake_case)
	EdgesJSON   datatypes.JSON `gorm:"column:edges_json;type:jsonb" json:"edges_json"`     // optional: {edges:[{from,to,type}]}

	// Text used for embedding + retrieval (stable description)
	ChainDoc string `gorm:"column:chain_doc;type:text" json:"chain_doc"`

	// Optional local embedding cache; vector store holds the real index.
	Embedding datatypes.JSON `gorm:"column:embedding;type:jsonb" json:"embedding"` // []float32
	VectorID  string         `gorm:"column:vector_id;index" json:"vector_id,omitempty"`

	Metadata datatypes.JSON `gorm:"column:metadata;type:jsonb" json:"metadata,omitempty"`

	CreatedAt time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (ChainSignature) TableName() string { return "chain_signature" }










