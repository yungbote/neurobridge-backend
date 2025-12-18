package learning

import (
	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"time"
)

type Path struct {
	ID          uuid.UUID      `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	UserID      *uuid.UUID     `gorm:"type:uuid;index" json:"user_id,omitempty"` // nil if template/global
	Title       string         `gorm:"column:title;not null" json:"title"`
	Description string         `gorm:"column:description;type:text" json:"description,omitempty"`
	Status      string         `gorm:"column:status;not null;default:'draft';index" json:"status"` // draft|active|archived
	Metadata    datatypes.JSON `gorm:"column:metadata;type:jsonb" json:"metadata,omitempty"`
	CreatedAt   time.Time      `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt   time.Time      `gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (Path) TableName() string { return "path" }

type PathNode struct {
	ID     uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	PathID uuid.UUID `gorm:"type:uuid;not null;index:idx_path_node,unique,priority:1" json:"path_id"`
	Path   *Path     `gorm:"constraint:OnDelete:CASCADE;foreignKey:PathID;references:ID" json:"path,omitempty"`
	Index  int       `gorm:"column:index;not null;index:idx_path_node,unique,priority:2" json:"index"`
	Title  string    `gorm:"column:title;not null" json:"title"`
	// If you later want a DAG, keep parent pointers or an edge table.
	ParentNodeID *uuid.UUID `gorm:"type:uuid;column:parent_node_id;index" json:"parent_node_id,omitempty"`
	// Gating: prerequisites / mastery thresholds / spaced repetition schedule hints
	Gating    datatypes.JSON `gorm:"column:gating;type:jsonb" json:"gating,omitempty"`
	Metadata  datatypes.JSON `gorm:"column:metadata;type:jsonb" json:"metadata,omitempty"`
	CreatedAt time.Time      `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (PathNode) TableName() string { return "path_node" }

type PathNodeActivity struct {
	ID         uuid.UUID      `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	PathNodeID uuid.UUID      `gorm:"type:uuid;not null;index:idx_path_node_activity,unique,priority:1" json:"path_node_id"`
	PathNode   *PathNode      `gorm:"constraint:OnDelete:CASCADE;foreignKey:PathNodeID;references:ID" json:"path_node,omitempty"`
	ActivityID uuid.UUID      `gorm:"type:uuid;not null;index:idx_path_node_activity,unique,priority:2" json:"activity_id"`
	Activity   *Activity      `gorm:"constraint:OnDelete:CASCADE;foreignKey:ActivityID;references:ID" json:"activity,omitempty"`
	Rank       int            `gorm:"column:rank;not null;default:0" json:"rank"`
	IsPrimary  bool           `gorm:"column:is_primary;not null;default:true" json:"is_primary"`
	CreatedAt  time.Time      `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt  time.Time      `gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt  gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (PathNodeActivity) TableName() string { return "path_node_activity" }
