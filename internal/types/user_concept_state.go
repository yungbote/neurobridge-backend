package types

import (
	"time"
	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type UserConceptState struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	UserID   uuid.UUID `gorm:"type:uuid;not null;index:idx_user_concept_state,unique,priority:1" json:"user_id"`
	ConceptID uuid.UUID `gorm:"type:uuid;not null;index:idx_user_concept_state,unique,priority:2" json:"concept_id"`
	Mastery    float64 `gorm:"column:mastery;not null;default:0" json:"mastery"`       // 0..1
	Confidence float64 `gorm:"column:confidence;not null;default:0" json:"confidence"` // 0..1
	LastSeenAt   *time.Time `gorm:"column:last_seen_at;index" json:"last_seen_at,omitempty"`
	NextReviewAt *time.Time `gorm:"column:next_review_at;index" json:"next_review_at,omitempty"`
	DecayRate float64 `gorm:"column:decay_rate;not null;default:0" json:"decay_rate"`
	// Store misconceptions, common errors, etc.
	Misconceptions datatypes.JSON `gorm:"column:misconceptions;type:jsonb" json:"misconceptions,omitempty"`
	// Lightweight counters for bandit / scoring
	Attempts int `gorm:"column:attempts;not null;default:0" json:"attempts"`
	Correct  int `gorm:"column:correct;not null;default:0" json:"correct"`
	CreatedAt time.Time      `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (UserConceptState) TableName() string { return "user_concept_state" }










