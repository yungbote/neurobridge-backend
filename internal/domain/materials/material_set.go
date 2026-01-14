package materials

import (
	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/domain/user"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"time"
)

type MaterialSet struct {
	ID          uuid.UUID  `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	UserID      uuid.UUID  `gorm:"type:uuid;not null;index" json:"user_id"`
	User        *user.User `gorm:"constraint:OnDelete:CASCADE;foreignKey:UserID;references:ID" json:"user,omitempty"`
	Title       string     `gorm:"column:title" json:"title"`
	Description string     `gorm:"column:description" json:"description"`
	Status      string     `gorm:"column:status;not null;default:'pending'" json:"status"`

	// SourceMaterialSetID links a derived material set back to the originating upload batch.
	// When present, this set's membership is defined by material_set_file rows (not by material_file.material_set_id).
	SourceMaterialSetID *uuid.UUID `gorm:"type:uuid;column:source_material_set_id;index" json:"source_material_set_id,omitempty"`

	// Metadata stores derivation/provenance and other auxiliary info.
	Metadata datatypes.JSON `gorm:"column:metadata;type:jsonb" json:"metadata,omitempty"`

	CreatedAt time.Time      `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (MaterialSet) TableName() string { return "material_set" }
