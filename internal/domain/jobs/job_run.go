package jobs

import (
	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"time"
)

type JobRun struct {
	ID          uuid.UUID      `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	OwnerUserID uuid.UUID      `gorm:"type:uuid;not null;index" json:"owner_user_id"`
	JobType     string         `gorm:"column:job_type;not null;index" json:"job_type"`
	EntityType  string         `gorm:"column:entity_type;index" json:"entity_type,omitempty"`
	EntityID    *uuid.UUID     `gorm:"type:uuid;column:entity_id;index" json:"entity_id,omitempty"`
	Status      string         `gorm:"column:status;not null;index" json:"status"`
	Stage       string         `gorm:"column:stage;not null;index" json:"stage"`
	Progress    int            `gorm:"column:progress;not null;default:0" json:"progress"`
	Attempts    int            `gorm:"column:attempts;not null;default:0" json:"attempts"`
	Error       string         `gorm:"column:error" json:"error,omitempty"`
	LockedAt    *time.Time     `gorm:"column:locked_at;index" json:"locked_at,omitempty"`
	HeartbeatAt *time.Time     `gorm:"column:heartbeat_at;index" json:"heartbeat_at,omitempty"`
	LastErrorAt *time.Time     `gorm:"column:last_error_at;index" json:"last_error_at,omitempty"`
	Payload     datatypes.JSON `gorm:"column:payload;type:jsonb" json:"payload"`
	Result      datatypes.JSON `gorm:"column:result;type:jsonb" json:"result"`
	CreatedAt   time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt   time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (JobRun) TableName() string { return "job_run" }
