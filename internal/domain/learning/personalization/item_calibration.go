package personalization

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ItemCalibration stores global IRT/BKT calibration parameters for an assessment item.
type ItemCalibration struct {
	ID       uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	ItemID   string    `gorm:"column:item_id;not null;uniqueIndex:idx_item_calibration,priority:1" json:"item_id"`
	ItemType string    `gorm:"column:item_type;not null;uniqueIndex:idx_item_calibration,priority:2" json:"item_type"`

	ConceptID *uuid.UUID `gorm:"type:uuid;index" json:"concept_id,omitempty"`

	Difficulty     float64 `gorm:"column:difficulty;not null;default:0" json:"difficulty"`
	Discrimination float64 `gorm:"column:discrimination;not null;default:1" json:"discrimination"`
	Guess          float64 `gorm:"column:guess;not null;default:0" json:"guess"`
	Slip           float64 `gorm:"column:slip;not null;default:0" json:"slip"`

	Count   int `gorm:"column:count;not null;default:0" json:"count"`
	Correct int `gorm:"column:correct;not null;default:0" json:"correct"`

	LastEventAt *time.Time     `gorm:"column:last_event_at;index" json:"last_event_at,omitempty"`
	CreatedAt   time.Time      `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt   time.Time      `gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (ItemCalibration) TableName() string { return "item_calibration" }
