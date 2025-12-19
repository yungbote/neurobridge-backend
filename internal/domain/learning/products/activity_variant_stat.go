package products

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Per-variant stats.
type ActivityVariantStat struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	ActivityVariantID uuid.UUID `gorm:"type:uuid;not null;uniqueIndex" json:"activity_variant_id"`

	Completions int `gorm:"column:completions;not null;default:0" json:"completions"`
	Starts      int `gorm:"column:starts;not null;default:0" json:"starts"`

	ThumbsUp   int `gorm:"column:thumbs_up;not null;default:0" json:"thumbs_up"`
	ThumbsDown int `gorm:"column:thumbs_down;not null;default:0" json:"thumbs_down"`

	AvgScore   float64 `gorm:"column:avg_score;not null;default:0" json:"avg_score"`
	AvgDwellMS int     `gorm:"column:avg_dwell_ms;not null;default:0" json:"avg_dwell_ms"`

	LastObservedAt *time.Time     `gorm:"column:last_observed_at;index" json:"last_observed_at,omitempty"`
	UpdatedAt      time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (ActivityVariantStat) TableName() string { return "activity_variant_stat" }
