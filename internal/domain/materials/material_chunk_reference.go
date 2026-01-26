package materials

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// MaterialChunkReference links an in-text citation in a chunk to a bibliography reference.
type MaterialChunkReference struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	MaterialChunkID     uuid.UUID        `gorm:"type:uuid;not null;index:idx_material_chunk_ref,unique,priority:1" json:"material_chunk_id"`
	MaterialChunk       *MaterialChunk   `gorm:"constraint:OnDelete:CASCADE;foreignKey:MaterialChunkID;references:ID" json:"material_chunk,omitempty"`
	MaterialReferenceID uuid.UUID        `gorm:"type:uuid;not null;index:idx_material_chunk_ref,unique,priority:2" json:"material_reference_id"`
	MaterialReference   *MaterialReference `gorm:"constraint:OnDelete:CASCADE;foreignKey:MaterialReferenceID;references:ID" json:"material_reference,omitempty"`

	CitationText string         `gorm:"type:text;not null;index" json:"citation_text"`
	CitationKind string         `gorm:"type:text;not null;default:'unknown';index" json:"citation_kind"`
	Metadata     datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'" json:"metadata"`

	CreatedAt time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (MaterialChunkReference) TableName() string { return "material_chunk_reference" }
