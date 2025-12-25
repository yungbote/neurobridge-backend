package materials

import (
	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"time"
)

type MaterialAsset struct {
	ID             uuid.UUID     `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	MaterialFileID uuid.UUID     `gorm:"type:uuid;not null;index" json:"material_file_id"`
	MaterialFile   *MaterialFile `gorm:"constraint:OnDelete:CASCADE;foreignKey:MaterialFileID;references:ID" json:"material_file,omitempty"`

	Kind       string         `gorm:"column:kind;not null;index" json:"kind"` // original|pdf_page|ppt_slide|frame|audio
	StorageKey string         `gorm:"column:storage_key;not null;index" json:"storage_key"`
	URL        string         `gorm:"column:url" json:"url"`
	Page       *int           `gorm:"column:page;index" json:"page,omitempty"`
	StartSec   *float64       `gorm:"column:start_sec;index" json:"start_sec,omitempty"`
	EndSec     *float64       `gorm:"column:end_sec;index" json:"end_sec,omitempty"`
	Metadata   datatypes.JSON `gorm:"column:metadata;type:jsonb" json:"metadata"`

	CreatedAt time.Time      `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (MaterialAsset) TableName() string { return "material_asset" }
