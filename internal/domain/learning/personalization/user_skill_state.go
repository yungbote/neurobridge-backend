package personalization

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// UserSkillState tracks per-user ability (theta) for a concept in IRT space.
type UserSkillState struct {
	ID        uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	UserID    uuid.UUID `gorm:"type:uuid;not null;index:idx_user_skill_state,unique,priority:1" json:"user_id"`
	ConceptID uuid.UUID `gorm:"type:uuid;not null;index:idx_user_skill_state,unique,priority:2" json:"concept_id"`

	Theta float64 `gorm:"column:theta;not null;default:0" json:"theta"`
	Sigma float64 `gorm:"column:sigma;not null;default:1" json:"sigma"`
	Count int     `gorm:"column:count;not null;default:0" json:"count"`

	LastEventAt *time.Time     `gorm:"column:last_event_at;index" json:"last_event_at,omitempty"`
	CreatedAt   time.Time      `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt   time.Time      `gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (UserSkillState) TableName() string { return "user_skill_state" }
