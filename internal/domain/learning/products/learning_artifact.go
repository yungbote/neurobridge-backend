package products

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// LearningArtifact stores per-stage cache metadata keyed by material set/path inputs.
// PathID uses uuid.Nil to represent set-level artifacts.
type LearningArtifact struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	OwnerUserID   uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:idx_learning_artifact_key" json:"owner_user_id"`
	MaterialSetID uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:idx_learning_artifact_key" json:"material_set_id"`
	PathID        uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:idx_learning_artifact_key" json:"path_id"`
	ArtifactType  string    `gorm:"type:text;not null;uniqueIndex:idx_learning_artifact_key" json:"artifact_type"`

	InputHash string         `gorm:"type:text;not null;index" json:"input_hash"`
	Version   int            `gorm:"not null;default:1" json:"version"`
	Metadata  datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'" json:"metadata"`

	CreatedAt time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (LearningArtifact) TableName() string { return "learning_artifact" }
