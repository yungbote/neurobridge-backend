package types

import (
	"time"
	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type Asset struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	Kind       string `gorm:"column:kind;not null;index" json:"kind"`           // image|diagram|video|audio|pdf_page|frame|original
	StorageKey string `gorm:"column:storage_key;not null;index" json:"storage_key"`
	URL        string `gorm:"column:url" json:"url,omitempty"`
	// Ownership (polymorphic)
	OwnerType string     `gorm:"column:owner_type;not null;index:idx_asset_owner,priority:1" json:"owner_type"` // material_file|activity_variant|concept|...
	OwnerID   uuid.UUID  `gorm:"type:uuid;column:owner_id;not null;index:idx_asset_owner,priority:2" json:"owner_id"`
	Page     *int     `gorm:"column:page;index" json:"page,omitempty"`
	StartSec *float64 `gorm:"column:start_sec;index" json:"start_sec,omitempty"`
	EndSec   *float64 `gorm:"column:end_sec;index" json:"end_sec,omitempty"`
	Metadata  datatypes.JSON `gorm:"column:metadata;type:jsonb" json:"metadata,omitempty"`
	CreatedAt time.Time      `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (Asset) TableName() string { return "asset" }










