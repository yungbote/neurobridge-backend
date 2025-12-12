package types

import (
  "time"
  "github.com/google/uuid"
  "gorm.io/datatypes"
  "gorm.io/gorm"
)

type AICallLog struct {
  gorm.Model
  ID                  uuid.UUID         `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
  UserID              *uuid.UUID        `gorm:"type:uuid;index" json:"user_id,omitempty"`
  ContextID           *uuid.UUID        `gorm:"type:uuid;index" json:"context_id,omitempty"`
  CallType            string            `gorm:"column:call_type;not null" json:"call_type"`
  Model               string            `gorm:"column:model;not null" json:"model"`
  Prompt              string            `gorm:"column:prompt" json:"prompt"`
  Response            string            `gorm:"column:response" json:"response"`
  Success             bool              `gorm:"column:success;not null" json:"success"`
  Error               string            `gorm:"column:error" json:"error"`
  Usage               datatypes.JSON    `gorm:"type:jsonb;column:usage" json:"usage"`
  CreatedAt           time.Time         `gorm:"not null;default:now()" json:"created_at"`
  UpdatedAt           time.Time         `gorm:"not null;default:now()" json:"updated_at"`
}

func (AICallLog) TableName() string {
  return "ai_call_log"
}










