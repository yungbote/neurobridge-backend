package materials

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// MaterialFileSignature stores extracted, durable metadata per material_file.
// This is used for premium path grouping and structure reasoning.
type MaterialFileSignature struct {
	ID             uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	MaterialFileID uuid.UUID `gorm:"type:uuid;not null;uniqueIndex" json:"material_file_id"`
	MaterialSetID  uuid.UUID `gorm:"type:uuid;not null;index" json:"material_set_id"`

	Version int `gorm:"column:version;not null;default:1" json:"version"`

	Language    string         `gorm:"column:language;index" json:"language,omitempty"`
	Quality     datatypes.JSON `gorm:"column:quality;type:jsonb" json:"quality,omitempty"`
	Difficulty  string         `gorm:"column:difficulty;index" json:"difficulty,omitempty"`
	DomainTags  datatypes.JSON `gorm:"column:domain_tags;type:jsonb" json:"domain_tags,omitempty"`
	Topics      datatypes.JSON `gorm:"column:topics;type:jsonb" json:"topics,omitempty"`
	ConceptKeys datatypes.JSON `gorm:"column:concept_keys;type:jsonb" json:"concept_keys,omitempty"`

	SummaryMD        string         `gorm:"column:summary_md;type:text" json:"summary_md,omitempty"`
	SummaryEmbedding datatypes.JSON `gorm:"column:summary_embedding;type:jsonb" json:"summary_embedding,omitempty"`

	OutlineJSON       datatypes.JSON `gorm:"column:outline_json;type:jsonb" json:"outline_json,omitempty"`
	OutlineConfidence float64        `gorm:"column:outline_confidence" json:"outline_confidence,omitempty"`

	Citations   datatypes.JSON `gorm:"column:citations;type:jsonb" json:"citations,omitempty"`
	Fingerprint string         `gorm:"column:fingerprint;index" json:"fingerprint,omitempty"`

	CreatedAt time.Time      `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (MaterialFileSignature) TableName() string { return "material_file_signature" }
