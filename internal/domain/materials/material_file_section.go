package materials

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// MaterialFileSection stores per-outline node metadata + embeddings for a file.
type MaterialFileSection struct {
	ID             uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	MaterialFileID uuid.UUID `gorm:"type:uuid;not null;index;index:idx_material_file_section,unique,priority:1" json:"material_file_id"`

	SectionIndex int    `gorm:"column:section_index;not null;index;index:idx_material_file_section,unique,priority:2" json:"section_index"`
	Title        string `gorm:"column:title" json:"title,omitempty"`
	Path         string `gorm:"column:path" json:"path,omitempty"`

	StartPage *int     `gorm:"column:start_page;index" json:"start_page,omitempty"`
	EndPage   *int     `gorm:"column:end_page;index" json:"end_page,omitempty"`
	StartSec  *float64 `gorm:"column:start_sec;index" json:"start_sec,omitempty"`
	EndSec    *float64 `gorm:"column:end_sec;index" json:"end_sec,omitempty"`

	TextExcerpt string         `gorm:"column:text_excerpt;type:text" json:"text_excerpt,omitempty"`
	Embedding   datatypes.JSON `gorm:"column:embedding;type:jsonb" json:"embedding,omitempty"`

	Metadata datatypes.JSON `gorm:"column:metadata;type:jsonb" json:"metadata,omitempty"`

	CreatedAt time.Time      `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (MaterialFileSection) TableName() string { return "material_file_section" }
