package learning

import (
	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/domain/user"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"time"
)

type TopicMastery struct {
	ID         uuid.UUID      `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	UserID     uuid.UUID      `gorm:"type:uuid;not null;index" json:"user_id"`
	User       *user.User     `gorm:"constraint:OnDelete:CASCADE;foreignKey:UserID;references:ID" json:"user,omitempty"`
	Topic      string         `gorm:"column:topic;not null;index:idx_user_topic,unique" json:"topic"`
	Mastery    float64        `gorm:"column:mastery;not null" json:"mastery"`
	Metadata   datatypes.JSON `gorm:"type:jsonb;column:metadata" json:"metadata"`
	LastUpdate time.Time      `gorm:"column:last_update;not null;default:now()" json:"last_update"`
	CreatedAt  time.Time      `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt  time.Time      `gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt  gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (TopicMastery) TableName() string { return "topic_mastery" }
