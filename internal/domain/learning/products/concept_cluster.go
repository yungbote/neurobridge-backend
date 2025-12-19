package products

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// Concept clusters (families).
type ConceptCluster struct {
	ID      uuid.UUID  `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	Scope   string     `gorm:"column:scope;not null;index:idx_concept_cluster_scope" json:"scope"`
	ScopeID *uuid.UUID `gorm:"type:uuid;column:scope_id;index:idx_concept_cluster_scope" json:"scope_id,omitempty"`
	Label   string     `gorm:"column:label;not null;index" json:"label"`

	Metadata  datatypes.JSON `gorm:"column:metadata;type:jsonb" json:"metadata"`
	Embedding datatypes.JSON `gorm:"column:embedding;type:jsonb" json:"embedding"`
	VectorID  string         `gorm:"column:vector_id;index" json:"vector_id,omitempty"`

	CreatedAt time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (ConceptCluster) TableName() string { return "concept_cluster" }

type ConceptClusterMember struct {
	ID        uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	ClusterID uuid.UUID `gorm:"type:uuid;not null;index:idx_cluster_member,unique,priority:1" json:"cluster_id"`
	ConceptID uuid.UUID `gorm:"type:uuid;not null;index:idx_cluster_member,unique,priority:2" json:"concept_id"`
	Weight    float64   `gorm:"column:weight;not null;default:1" json:"weight"`

	CreatedAt time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (ConceptClusterMember) TableName() string { return "concept_cluster_member" }
