package products

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// User library index.
type UserLibraryIndex struct {
	ID            uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	UserID        uuid.UUID `gorm:"type:uuid;not null;index:idx_user_library,unique,priority:1" json:"user_id"`
	MaterialSetID uuid.UUID `gorm:"type:uuid;not null;index:idx_user_library,unique,priority:2" json:"material_set_id"`

	CourseID *uuid.UUID `gorm:"type:uuid;index" json:"course_id,omitempty"`
	PathID   *uuid.UUID `gorm:"type:uuid;index" json:"path_id,omitempty"`

	Tags              datatypes.JSON `gorm:"column:tags;type:jsonb" json:"tags"`
	ConceptClusterIDs datatypes.JSON `gorm:"column:concept_cluster_ids;type:jsonb" json:"concept_cluster_ids"`

	CreatedAt time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (UserLibraryIndex) TableName() string { return "user_library_index" }
