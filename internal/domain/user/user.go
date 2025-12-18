package user

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type User struct {
	ID              uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	Email           string    `gorm:"uniqueIndex;not null;column:email" json:"email"`
	Password        string    `gorm:"not null;column:password" json:"-"`
	FirstName       string    `gorm:"not null;column:first_name" json:"first_name"`
	LastName        string    `gorm:"not null;column:last_name" json:"last_name"`
	AvatarBucketKey string    `gorm:"column:avatar_bucket_key" json:"avatar_bucket_key"`
	AvatarURL       string    `gorm:"column:avatar_url" json:"avatar_url"`

	// FIX: preferred_theme was misspelled in gorm tag
	PreferredTheme string `gorm:"column:preferred_theme" json:"preferred_theme"`

	CreatedAt time.Time      `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (User) TableName() string { return "user" }
