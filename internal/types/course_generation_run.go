package types

import (
	"time"
	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type CourseGenerationRun struct {
	ID            uuid.UUID			 `gorm:"type:uuid;primaryKey" json:"id"`
	UserID        uuid.UUID			 `gorm:"type:uuid;not null;index" json:"user_id"`
	MaterialSetID uuid.UUID			 `gorm:"type:uuid;not null;index" json:"material_set_id"`
	CourseID      uuid.UUID			 `gorm:"type:uuid;not null;index" json:"course_id"`
	Status				string				 `gorm:"column:status;not null;index" json:"status"` // queued|running|succeeded|failed
	Stage					string				 `gorm:"column:stage;not null;index" json:"stage"`   // ingest|embed|metadata|blueprint|lessons|quizzes|done
	Progress			int						 `gorm:"column:progress;not null;default:0" json:"progress"`
	Attempts			int						 `gorm:"column:attempts;not null;default:0" json:"attempts"`
	Error					string				 `gorm:"column:error" json:"error"`
	LastErrorAt		*time.Time		 `gorm:"column:last_error_at" json:"last_error_at,omitempty"`
	LockedAt			*time.Time		 `gorm:"column:locked_at;index" json:"locked_at,omitempty"`
	HeartbeatAt		*time.Time		 `gorm:"column:heartbeat_at;index" json:"heartbeat_at,omitempty"`
	Metadata			datatypes.JSON `gorm:"type:jsonb;column:metadata" json:"metadata"`
	CreatedAt			time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt			time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt			gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (CourseGenerationRun) TableName() string { return "course_generation_run" }










