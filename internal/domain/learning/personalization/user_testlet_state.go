package personalization

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// UserTestletState stores aggregate performance/uncertainty for a testlet (group of items).
type UserTestletState struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	UserID      uuid.UUID `gorm:"type:uuid;not null;index:idx_user_testlet_state,unique,priority:1" json:"user_id"`
	TestletID   string    `gorm:"column:testlet_id;not null;index:idx_user_testlet_state,unique,priority:2" json:"testlet_id"`
	TestletType string    `gorm:"column:testlet_type;not null;index:idx_user_testlet_state,unique,priority:3" json:"testlet_type"`

	Attempts int     `gorm:"column:attempts;not null;default:0" json:"attempts"`
	Correct  int     `gorm:"column:correct;not null;default:0" json:"correct"`
	BetaA    float64 `gorm:"column:beta_a;not null;default:1" json:"beta_a"`
	BetaB    float64 `gorm:"column:beta_b;not null;default:1" json:"beta_b"`
	EMA      float64 `gorm:"column:ema;not null;default:0" json:"ema"`

	LastSeenAt *time.Time `gorm:"column:last_seen_at;index" json:"last_seen_at,omitempty"`
	CreatedAt  time.Time  `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt  time.Time  `gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt  gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (UserTestletState) TableName() string { return "user_testlet_state" }
