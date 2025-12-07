package types

import (
  "time"
  "gorm.io/gorm"
  "github.com/google/uuid"
)

type User struct {
  gorm.Model
  ID                uuid.UUID       `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
  Email             string          `gorm:"uniqueIndex;not null;column:email" json:"email"`
  Password          string          `gorm:"not null;column:password" json:"-"`
  FirstName         string          `gorm:"not null;column:first_name" json:"first_name"`
  LastName          string          `gorm:"not null;column:last_name" json:"last_name"`
  AvatarBucketKey   string          `gorm:"column:avatar_bucket_key" json:"avatar_bucket_key"`
  AvatarURL         string          `gorm:"column:avatar_url" json:"avatar_url"`
  CreatedAt         time.Time       `gorm:"not null;default:now()" json:"created_at"`
  UpdatedAt         time.Time       `gorm:"not null;default:now()" json:"updated_at"`
}

func (User) TableName() string {
  return "user"
}
