package types

import (
	"time"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type ActivityCitation struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	ActivityVariantID uuid.UUID       `gorm:"type:uuid;not null;index:idx_activity_citation,unique,priority:1" json:"activity_variant_id"`
	ActivityVariant   *ActivityVariant `gorm:"constraint:OnDelete:CASCADE;foreignKey:ActivityVariantID;references:ID" json:"activity_variant,omitempty"`
	MaterialChunkID uuid.UUID      `gorm:"type:uuid;not null;index:idx_activity_citation,unique,priority:2" json:"material_chunk_id"`
	MaterialChunk   *MaterialChunk `gorm:"constraint:OnDelete:CASCADE;foreignKey:MaterialChunkID;references:ID" json:"material_chunk,omitempty"`
	// Optional: store why this chunk was used (retrieval score, excerpt offsets, etc.)
	Kind string `gorm:"column:kind;index" json:"kind,omitempty"` // grounding|example|definition|table|figure
	CreatedAt time.Time      `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (ActivityCitation) TableName() string { return "activity_citation" }










