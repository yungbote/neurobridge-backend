package core

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type Concept struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	Scope   string     `gorm:"column:scope;not null;default:'path';index:idx_concept_scope;index:idx_concept_scope_key,unique,priority:1" json:"scope"`
	ScopeID *uuid.UUID `gorm:"type:uuid;column:scope_id;index:idx_concept_scope;index:idx_concept_scope_key,unique,priority:2" json:"scope_id,omitempty"`

	ParentID *uuid.UUID `gorm:"type:uuid;column:parent_id;index" json:"parent_id,omitempty"`
	Parent   *Concept   `gorm:"constraint:OnDelete:SET NULL;foreignKey:ParentID;references:ID" json:"parent,omitempty"`

	Depth     int `gorm:"column:depth;not null;default:0" json:"depth"`
	SortIndex int `gorm:"column:sort_index;not null;default:0" json:"sort_index"`

	Key       string         `gorm:"column:key;not null;index:idx_concept_scope_key,unique,priority:3" json:"key"`
	Name      string         `gorm:"column:name;not null" json:"name"`
	Summary   string         `gorm:"column:summary;type:text" json:"summary,omitempty"`
	KeyPoints datatypes.JSON `gorm:"column:key_points;type:jsonb" json:"key_points"`

	VectorID string         `gorm:"column:vector_id;index" json:"vector_id,omitempty"`
	Metadata datatypes.JSON `gorm:"column:metadata;type:jsonb" json:"metadata,omitempty"`

	CreatedAt time.Time      `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (Concept) TableName() string { return "concept" }
