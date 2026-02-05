package personalization

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// UserModelAlert stores model-quality alerts (e.g., calibration drift) per user + concept.
type UserModelAlert struct {
	ID        uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	UserID    uuid.UUID `gorm:"type:uuid;not null;index:idx_user_model_alert,priority:1;uniqueIndex:idx_user_model_alert_key,priority:1" json:"user_id"`
	ConceptID uuid.UUID `gorm:"type:uuid;not null;index:idx_user_model_alert,priority:2;uniqueIndex:idx_user_model_alert_key,priority:2" json:"concept_id"`

	Kind     string  `gorm:"column:kind;type:text;not null;index;uniqueIndex:idx_user_model_alert_key,priority:3" json:"kind"`
	Severity string  `gorm:"column:severity;type:text;not null;default:'info'" json:"severity"`
	Score    float64 `gorm:"column:score;not null;default:0" json:"score"`

	Details datatypes.JSON `gorm:"column:details;type:jsonb" json:"details,omitempty"`

	FirstSeenAt *time.Time `gorm:"column:first_seen_at;index" json:"first_seen_at,omitempty"`
	LastSeenAt  *time.Time `gorm:"column:last_seen_at;index" json:"last_seen_at,omitempty"`
	Occurrences int        `gorm:"column:occurrences;not null;default:0" json:"occurrences"`
	ResolvedAt  *time.Time `gorm:"column:resolved_at;index" json:"resolved_at,omitempty"`

	CreatedAt time.Time      `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (UserModelAlert) TableName() string { return "user_model_alert" }
