package materials

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// GlobalEntity links equivalent entities across a user's material sets.
type GlobalEntity struct {
	ID     uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	UserID uuid.UUID `gorm:"type:uuid;not null;index:idx_global_entity_user_key,unique,priority:1;index" json:"user_id"`

	Key         string         `gorm:"type:text;not null;index:idx_global_entity_user_key,unique,priority:2" json:"key"`
	Name        string         `gorm:"type:text;not null;index" json:"name"`
	Type        string         `gorm:"type:text;not null;default:'unknown';index" json:"type"`
	Description string         `gorm:"type:text;not null;default:''" json:"description"`
	Aliases     datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'" json:"aliases"`
	Embedding   datatypes.JSON `gorm:"type:jsonb" json:"embedding,omitempty"`
	Metadata    datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'" json:"metadata"`

	CreatedAt time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (GlobalEntity) TableName() string { return "global_entity" }
