package materials

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type MaterialChunk struct {
	ID             uuid.UUID     `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	MaterialFileID uuid.UUID     `gorm:"type:uuid;not null;index" json:"material_file_id"`
	MaterialFile   *MaterialFile `gorm:"constraint:OnDelete:CASCADE;foreignKey:MaterialFileID;references:ID" json:"material_file,omitempty"`

	Index     int            `gorm:"column:index;not null" json:"index"`
	Text      string         `gorm:"column:text;type:text;not null" json:"text"`
	Embedding datatypes.JSON `gorm:"type:jsonb;column:embedding" json:"embedding"`

	// NEW: queryable provenance (stop hiding these in json)
	Kind       string   `gorm:"column:kind;index" json:"kind,omitempty"`
	Provider   string   `gorm:"column:provider;index" json:"provider,omitempty"`
	Page       *int     `gorm:"column:page;index" json:"page,omitempty"`
	StartSec   *float64 `gorm:"column:start_sec;index" json:"start_sec,omitempty"`
	EndSec     *float64 `gorm:"column:end_sec;index" json:"end_sec,omitempty"`
	SpeakerTag *int     `gorm:"column:speaker_tag;index" json:"speaker_tag,omitempty"`
	Confidence *float64 `gorm:"column:confidence" json:"confidence,omitempty"`
	AssetKey   string   `gorm:"column:asset_key;index" json:"asset_key,omitempty"`

	// still keep for extras
	Metadata datatypes.JSON `gorm:"type:jsonb;column:metadata" json:"metadata"`

	CreatedAt time.Time      `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (MaterialChunk) TableName() string { return "material_chunk" }
