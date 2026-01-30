package personalization

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// UserConceptModel stores structural understanding for a user + canonical concept.
type UserConceptModel struct {
	ID                 uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	UserID             uuid.UUID `gorm:"type:uuid;not null;index:idx_user_concept_model,unique,priority:1" json:"user_id"`
	CanonicalConceptID uuid.UUID `gorm:"type:uuid;not null;index:idx_user_concept_model,unique,priority:2;index" json:"canonical_concept_id"`

	ModelVersion int `gorm:"column:model_version;not null;default:1" json:"model_version"`

	ActiveFrames     datatypes.JSON `gorm:"column:active_frames;type:jsonb" json:"active_frames,omitempty"`
	Uncertainty      datatypes.JSON `gorm:"column:uncertainty;type:jsonb" json:"uncertainty,omitempty"`
	Assumptions      datatypes.JSON `gorm:"column:assumptions;type:jsonb" json:"assumptions,omitempty"`
	Support          datatypes.JSON `gorm:"column:support;type:jsonb" json:"support,omitempty"`
	LastStructuralAt *time.Time     `gorm:"column:last_structural_evidence_at;index" json:"last_structural_evidence_at,omitempty"`

	CreatedAt time.Time      `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (UserConceptModel) TableName() string { return "user_concept_model" }
