package personalization

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// MisconceptionResolutionState tracks proof-of-correction for a concept.
type MisconceptionResolutionState struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	UserID    uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:idx_misconception_resolution,priority:1" json:"user_id"`
	ConceptID uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:idx_misconception_resolution,priority:2" json:"concept_id"`

	Status string `gorm:"column:status;type:text;not null;default:'open';index" json:"status"`

	CorrectCount   int `gorm:"column:correct_count;not null;default:0" json:"correct_count"`
	IncorrectCount int `gorm:"column:incorrect_count;not null;default:0" json:"incorrect_count"`
	RequiredCorrect int `gorm:"column:required_correct;not null;default:2" json:"required_correct"`

	LastCorrectAt   *time.Time `gorm:"column:last_correct_at;index" json:"last_correct_at,omitempty"`
	LastIncorrectAt *time.Time `gorm:"column:last_incorrect_at;index" json:"last_incorrect_at,omitempty"`
	ResolvedAt      *time.Time `gorm:"column:resolved_at;index" json:"resolved_at,omitempty"`
	RelapsedAt      *time.Time `gorm:"column:relapsed_at;index" json:"relapsed_at,omitempty"`
	NextReviewAt    *time.Time `gorm:"column:next_review_at;index" json:"next_review_at,omitempty"`

	EvidenceJSON datatypes.JSON `gorm:"type:jsonb;column:evidence_json" json:"evidence_json,omitempty"`

	CreatedAt time.Time      `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (MisconceptionResolutionState) TableName() string { return "misconception_resolution_state" }
