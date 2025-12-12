package types

import (
  "time"
  "github.com/google/uuid"
  "gorm.io/datatypes"
  "gorm.io/gorm"
)

type MaterialChunk struct {
  gorm.Model
  ID              uuid.UUID         `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
  MaterialFileID  uuid.UUID         `gorm:"type:uuid;not null;index" json:"material_file_id"`
  MaterialFile    *MaterialFile     `gorm:"constraint:OnDelete:CASCADE;foreignKey:MaterialFileID;references:ID" json:"material_file,omitempty"`
  Index           int               `gorm:"column:index;not null" json:"index"`
  Text            string            `gorm:"column:text;not null" json:"text"`
  Embedding       datatypes.JSON    `gorm:"type:jsonb;column:embedding" json:"embedding"`
  Metadata        datatypes.JSON    `gorm:"type:jsonb;column:metadata" json:"metadata"`
  CreatedAt       time.Time         `gorm:"not null;default:now()" json:"created_at"`
  UpdatedAt       time.Time         `gorm:"not null;default:now()" json:"updated_at"`
}

func (MaterialChunk) TableName() string {
  return "material_chunk"
}










