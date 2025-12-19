package personalization

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type TopicStylePreference struct {
	ID     uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	UserID uuid.UUID `gorm:"type:uuid;not null;index:idx_topic_style_pref,unique,priority:1" json:"user_id"`
	Topic  string    `gorm:"column:topic;not null;index:idx_topic_style_pref,unique,priority:2" json:"topic"`

	Modality string `gorm:"column:modality;not null;index:idx_topic_style_pref,unique,priority:3" json:"modality"`
	Variant  string `gorm:"column:variant;not null;index:idx_topic_style_pref,unique,priority:4" json:"variant"`

	EMA float64 `gorm:"column:ema;not null;default:0" json:"ema"`
	N   int     `gorm:"column:n;not null;default:0" json:"n"`

	A float64 `gorm:"column:a;not null;default:1" json:"a"`
	B float64 `gorm:"column:b;not null;default:1" json:"b"`

	LastObservedAt *time.Time `gorm:"column:last_observed_at;index" json:"last_observed_at,omitempty"`

	UpdatedAt time.Time      `gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (TopicStylePreference) TableName() string { return "topic_style_preference" }
