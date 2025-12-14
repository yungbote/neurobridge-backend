package types

import (
	"time"
	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type MaterialFile struct {
	ID            uuid.UUID    `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	MaterialSetID uuid.UUID    `gorm:"type:uuid;not null;index" json:"material_set_id"`
	MaterialSet   *MaterialSet `gorm:"constraint:OnDelete:CASCADE;foreignKey:MaterialSetID;references:ID" json:"material_set,omitempty"`
	OriginalName string         `gorm:"column:original_name;not null" json:"original_name"`
	MimeType     string         `gorm:"column:mime_type" json:"mime_type"`
	SizeBytes    int64          `gorm:"column:size_bytes" json:"size_bytes"`
	StorageKey   string         `gorm:"column:storage_key;not null" json:"storage_key"`
	FileURL      string         `gorm:"column:file_url" json:"file_url"`
	Status       string         `gorm:"column:status;not null;default:'uploaded'" json:"status"`
	AIType       string         `gorm:"column:ai_type" json:"ai_type"`
	AITopics     datatypes.JSON `gorm:"column:ai_topics;type:jsonb" json:"ai_topics"`
	CreatedAt time.Time      `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (MaterialFile) TableName() string { return "material_file" }










