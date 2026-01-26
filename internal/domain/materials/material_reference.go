package materials

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// MaterialReference stores parsed bibliography entries for a material file.
type MaterialReference struct {
	ID             uuid.UUID     `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	MaterialFileID uuid.UUID     `gorm:"type:uuid;not null;index;index:idx_material_ref_file_label,unique,priority:1" json:"material_file_id"`
	MaterialFile   *MaterialFile `gorm:"constraint:OnDelete:CASCADE;foreignKey:MaterialFileID;references:ID" json:"material_file,omitempty"`

	Label   string         `gorm:"type:text;not null;index:idx_material_ref_file_label,unique,priority:2" json:"label"`
	Raw     string         `gorm:"type:text;not null" json:"raw"`
	Authors datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'" json:"authors"`
	Title   string         `gorm:"type:text;not null;default:''" json:"title"`
	Year    *int           `gorm:"column:year;index" json:"year,omitempty"`
	DOI     string         `gorm:"type:text;not null;default:'';index" json:"doi"`
	Metadata datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'" json:"metadata"`

	CreatedAt time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (MaterialReference) TableName() string { return "material_reference" }
