package products

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// RollbackEvent records structural rollback activity and outcomes.
type RollbackEvent struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	GraphVersionFrom string `gorm:"column:graph_version_from;type:text;index" json:"graph_version_from,omitempty"`
	GraphVersionTo   string `gorm:"column:graph_version_to;type:text;index" json:"graph_version_to,omitempty"`

	Trigger string `gorm:"column:trigger;type:text;index" json:"trigger,omitempty"`
	Status  string `gorm:"column:status;type:text;not null;default:'pending';index" json:"status"`

	InitiatedAt *time.Time `gorm:"column:initiated_at;index" json:"initiated_at,omitempty"`
	CompletedAt *time.Time `gorm:"column:completed_at;index" json:"completed_at,omitempty"`

	Notes datatypes.JSON `gorm:"column:notes;type:jsonb" json:"notes,omitempty"`

	CreatedAt time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (RollbackEvent) TableName() string { return "rollback_event" }
