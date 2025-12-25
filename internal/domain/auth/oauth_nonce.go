package auth

import (
	"github.com/google/uuid"
	"gorm.io/gorm"
	"time"
)

type OAuthNonce struct {
	ID        uuid.UUID      `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	Provider  string         `gorm:"not null;column:provider" json:"provider"`
	NonceHash string         `gorm:"not null;column:nonce_hash" json:"nonce_hash"`
	ExpiresAt time.Time      `gorm:"not null;column:expires_at" json:"expires_at"`
	UsedAt    *time.Time     `gorm:"column:used_at" json:"used_at,omitempty"`
	CreatedAt time.Time      `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (OAuthNonce) TableName() string { return "oauth_nonce" }
