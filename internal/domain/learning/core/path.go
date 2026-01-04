package core

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type Path struct {
	ID                    uuid.UUID      `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	UserID                *uuid.UUID     `gorm:"type:uuid;index" json:"user_id,omitempty"`
	Title                 string         `gorm:"column:title;not null" json:"title"`
	Description           string         `gorm:"column:description;type:text" json:"description,omitempty"`
	Status                string         `gorm:"column:status;not null;default:'draft';index" json:"status"`
	JobID                 *uuid.UUID     `gorm:"type:uuid;column:job_id;index" json:"job_id,omitempty"`
	Metadata              datatypes.JSON `gorm:"column:metadata;type:jsonb" json:"metadata,omitempty"`
	AvatarBucketKey       string         `gorm:"column:avatar_bucket_key" json:"avatar_bucket_key"`
	AvatarURL             string         `gorm:"column:avatar_url" json:"avatar_url"`
	AvatarSquareBucketKey string         `gorm:"column:avatar_square_bucket_key" json:"avatar_square_bucket_key"`
	AvatarSquareURL       string         `gorm:"column:avatar_square_url" json:"avatar_square_url"`
	ViewCount             int            `gorm:"column:view_count;not null;default:0" json:"view_count"`
	LastViewedAt          *time.Time     `gorm:"column:last_viewed_at;index" json:"last_viewed_at,omitempty"`
	ReadyAt               *time.Time     `gorm:"column:ready_at;index" json:"ready_at,omitempty"`
	CreatedAt             time.Time      `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt             time.Time      `gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt             gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (Path) TableName() string { return "path" }

type PathNode struct {
	ID     uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	PathID uuid.UUID `gorm:"type:uuid;not null;index:idx_path_node,unique,priority:1" json:"path_id"`
	Path   *Path     `gorm:"constraint:OnDelete:CASCADE;foreignKey:PathID;references:ID" json:"path,omitempty"`

	Index int    `gorm:"column:index;not null;index:idx_path_node,unique,priority:2" json:"index"`
	Title string `gorm:"column:title;not null" json:"title"`

	ParentNodeID          *uuid.UUID     `gorm:"type:uuid;column:parent_node_id;index" json:"parent_node_id,omitempty"`
	Gating                datatypes.JSON `gorm:"column:gating;type:jsonb" json:"gating,omitempty"`
	AvatarBucketKey       string         `gorm:"column:avatar_bucket_key" json:"avatar_bucket_key"`
	AvatarURL             string         `gorm:"column:avatar_url" json:"avatar_url"`
	AvatarSquareBucketKey string         `gorm:"column:avatar_square_bucket_key" json:"avatar_square_bucket_key"`
	AvatarSquareURL       string         `gorm:"column:avatar_square_url" json:"avatar_square_url"`
	Metadata              datatypes.JSON `gorm:"column:metadata;type:jsonb" json:"metadata,omitempty"`
	ContentJSON           datatypes.JSON `gorm:"column:content_json;type:jsonb" json:"content_json,omitempty"`

	CreatedAt time.Time      `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (PathNode) TableName() string { return "path_node" }
