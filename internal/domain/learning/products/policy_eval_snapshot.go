package products

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// PolicyEvalSnapshot stores off-policy evaluation metrics over a time window.
type PolicyEvalSnapshot struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	PolicyKey  string    `gorm:"column:policy_key;not null;index" json:"policy_key"`
	WindowStart time.Time `gorm:"column:window_start;not null;index" json:"window_start"`
	WindowEnd   time.Time `gorm:"column:window_end;not null;index" json:"window_end"`

	Samples int     `gorm:"column:samples;not null;default:0" json:"samples"`
	IPS     float64 `gorm:"column:ips;not null;default:0" json:"ips"`
	Lift    float64 `gorm:"column:lift;not null;default:0" json:"lift"`

	MetricsJSON datatypes.JSON `gorm:"column:metrics_json;type:jsonb" json:"metrics_json"`

	CreatedAt time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (PolicyEvalSnapshot) TableName() string { return "policy_eval_snapshot" }
