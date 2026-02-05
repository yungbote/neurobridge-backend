package personalization

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// UserConceptCalibration tracks expected vs observed correctness for per-concept calibration.
type UserConceptCalibration struct {
	ID        uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	UserID    uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:idx_user_concept_calibration,priority:1" json:"user_id"`
	ConceptID uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:idx_user_concept_calibration,priority:2;index" json:"concept_id"`

	Count       int     `gorm:"column:count;not null;default:0" json:"count"`
	ExpectedSum float64 `gorm:"column:expected_sum;not null;default:0" json:"expected_sum"`
	ObservedSum float64 `gorm:"column:observed_sum;not null;default:0" json:"observed_sum"`
	BrierSum    float64 `gorm:"column:brier_sum;not null;default:0" json:"brier_sum"`
	AbsErrSum   float64 `gorm:"column:abs_err_sum;not null;default:0" json:"abs_err_sum"`

	LastEventAt *time.Time `gorm:"column:last_event_at;index" json:"last_event_at,omitempty"`

	CreatedAt time.Time      `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (UserConceptCalibration) TableName() string { return "user_concept_calibration" }
