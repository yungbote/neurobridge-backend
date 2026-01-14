package materials

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// MaterialSetFile is a join table that allows a derived material_set to reference
// a subset of existing material_file rows (without duplicating files/chunks).
//
// Notes:
//   - Upload batches still own the physical files via material_file.material_set_id.
//   - Derived sets declare membership via this join table and point back to the source set
//     using material_set.source_material_set_id.
type MaterialSetFile struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	MaterialSetID uuid.UUID    `gorm:"type:uuid;not null;index:idx_material_set_file,unique,priority:1;index" json:"material_set_id"`
	MaterialSet   *MaterialSet `gorm:"constraint:OnDelete:CASCADE;foreignKey:MaterialSetID;references:ID" json:"material_set,omitempty"`

	MaterialFileID uuid.UUID     `gorm:"type:uuid;not null;index:idx_material_set_file,unique,priority:2;index" json:"material_file_id"`
	MaterialFile   *MaterialFile `gorm:"constraint:OnDelete:CASCADE;foreignKey:MaterialFileID;references:ID" json:"material_file,omitempty"`

	CreatedAt time.Time      `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (MaterialSetFile) TableName() string { return "material_set_file" }
