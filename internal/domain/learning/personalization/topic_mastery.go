package personalization

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type TopicMastery struct {
	ID     uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	UserID uuid.UUID `gorm:"type:uuid;not null;index:idx_topic_mastery,unique,priority:1" json:"user_id"`
	Topic  string    `gorm:"column:topic;not null;index:idx_topic_mastery,unique,priority:2" json:"topic"`

	Mastery    float64 `gorm:"column:mastery;not null;default:0" json:"mastery"`
	Confidence float64 `gorm:"column:confidence;not null;default:0" json:"confidence"`

	LastObservedAt *time.Time `gorm:"column:last_observed_at;index" json:"last_observed_at,omitempty"`

	CreatedAt time.Time      `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (TopicMastery) TableName() string { return "topic_mastery" }
