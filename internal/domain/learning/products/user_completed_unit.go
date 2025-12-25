package products

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// UserCompletedUnit is a compact, queryable record of “verifiably finished” learning units,
// keyed by chain_key (so we can avoid dwelling on already-mastered similar chains).
type UserCompletedUnit struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	UserID   uuid.UUID `gorm:"type:uuid;not null;index:idx_user_completed_unit,unique,priority:1" json:"user_id"`
	ChainKey string    `gorm:"column:chain_key;not null;index:idx_user_completed_unit,unique,priority:2" json:"chain_key"`

	CompletedAt *time.Time `gorm:"column:completed_at;index" json:"completed_at,omitempty"`

	// 0..1 confidence that this unit is completed/mastered (derived from evidence).
	CompletionConfidence float64 `gorm:"column:completion_confidence;not null;default:0" json:"completion_confidence"`

	// Optional learning metrics at completion time (for novelty/skip decisions).
	MasteryAt    float64 `gorm:"column:mastery_at;not null;default:0" json:"mastery_at"`
	AvgScore     float64 `gorm:"column:avg_score;not null;default:0" json:"avg_score"`
	TotalDwellMS int     `gorm:"column:total_dwell_ms;not null;default:0" json:"total_dwell_ms"`
	Attempts     int     `gorm:"column:attempts;not null;default:0" json:"attempts"`

	Metadata datatypes.JSON `gorm:"column:metadata;type:jsonb" json:"metadata,omitempty"`

	UpdatedAt time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (UserCompletedUnit) TableName() string { return "user_completed_unit" }
