package types

import (
	"time"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type TopicStylePreference struct {
	ID				uuid.UUID			 `gorm:"type:uuid;primaryKey" json:"id"`
	UserID		uuid.UUID			 `gorm:"type:uuid;not null;index:idx_user_topic_style,unique" json:"user_id"`
	Topic			string				 `gorm:"not null;index:idx_user_topic_style,unique" json:"topic"`
	Style			string				 `gorm:"not null;index:idx_user_topic_style,unique" json:"style"`
	Score			float64				 `gorm:"not null;default:0" json:"score"`
	N					int						 `gorm:"not null;default:0" json:"n"`
	UpdatedAt time.Time      `gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (TopicStylePreference) TableName() string { return "topic_style_preference" }










