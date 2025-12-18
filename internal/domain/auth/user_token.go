package auth

import (
	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/domain/user"
	"gorm.io/gorm"
	"time"
)

type UserToken struct {
	ID           uuid.UUID      `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	UserID       uuid.UUID      `gorm:"index;not null" json:"user_id"`
	User         *user.User     `gorm:"constraint:OnDelete:CASCADE;foreignKey:UserID;references:ID" json:"user,omitempty"`
	AccessToken  string         `gorm:"uniqueIndex;not null;column:access_token" json:"access_token"`
	RefreshToken string         `gorm:"uniqueIndex;not null;column:refresh_token" json:"refresh_token"`
	ExpiresAt    time.Time      `gorm:"column:expires_at" json:"expires_at"`
	CreatedAt    time.Time      `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt    time.Time      `gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (UserToken) TableName() string { return "user_token" }
