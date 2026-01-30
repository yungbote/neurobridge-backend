package personalization

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// UserMisconceptionInstance stores a concrete misconception signal for a user + canonical concept.
type UserMisconceptionInstance struct {
	ID                 uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	UserID             uuid.UUID `gorm:"type:uuid;not null;index:idx_user_misconception,priority:1;uniqueIndex:idx_user_misconception_key,priority:1" json:"user_id"`
	CanonicalConceptID uuid.UUID `gorm:"type:uuid;not null;index:idx_user_misconception,priority:2;index;uniqueIndex:idx_user_misconception_key,priority:2" json:"canonical_concept_id"`

	PatternID   *string `gorm:"column:pattern_id;type:text;uniqueIndex:idx_user_misconception_key,priority:3" json:"pattern_id,omitempty"`
	Description string  `gorm:"column:description;type:text;uniqueIndex:idx_user_misconception_key,priority:4" json:"description"`
	Status      string  `gorm:"column:status;not null;default:'active';index" json:"status"`
	Confidence  float64 `gorm:"column:confidence;not null;default:0" json:"confidence"`

	FirstSeenAt *time.Time `gorm:"column:first_seen_at;index" json:"first_seen_at,omitempty"`
	LastSeenAt  *time.Time `gorm:"column:last_seen_at;index" json:"last_seen_at,omitempty"`
	ClearedAt   *time.Time `gorm:"column:cleared_at;index" json:"cleared_at,omitempty"`

	Support datatypes.JSON `gorm:"column:support;type:jsonb" json:"support,omitempty"`

	CreatedAt time.Time      `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (UserMisconceptionInstance) TableName() string { return "user_misconception_instance" }
