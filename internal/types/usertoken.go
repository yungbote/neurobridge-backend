package types

import (
  "time"
  "gorm.io/gorm"
  "github.com/google/uuid"
)

type UserToken struct {
  gorm.Model
  ID            uuid.UUID       `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
  UserID        uuid.UUID       `gorm:"index;not null"`
  User          *User           `gorm:"constraint:OnDelete:CASCADE;foreignKey:UserID;references:ID"`
  AccessToken   string          `gorm:"uniqueIndex;not null;column:access_token" json:"access_token"`
  RefreshToken  string          `gorm:"uniqueIndex;not null;column:refresh_token" json:"refresh_token"`
  ExpiresAt     time.Time       `gorm:"column:expires_at" json:"expires_at"`
  CreatedAt     time.Time       `gorm:"not null;default:now()"`
  UpdatedAt     time.Time       `gorm:"not null;default:now()"`
}

func (UserToken) TableName() string {
  return "user_token"
}
