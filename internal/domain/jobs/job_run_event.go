package jobs

import (
	"time"
	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type JobEventKind string

const (
	JobEventCreated			JobEventKind = "created"
	JobEventProgress		JobEventKind = "progress"
	JobEventFailed			JobEventKind = "failed"
	JobEventSucceeded		JobEventKind = "succeeded"
)

// JobRunEvent is an append-only ledger of job status/progress messages.
// This is the canonical "timeline" for our frontend.
type JobRunEvent struct {
	ID						uuid.UUID					`gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	JobID					uuid.UUID					`gorm:"type:uuid;not null;index" json:"job_id"`
	OwnerUserID		uuid.UUID					`gorm:"type:uuid;not null;index" json:"owner_user_id"`
	JobType				string						`gorm:"column:job_type;not null;index" json:"job_type"`
	EntityType		string						`gorm:"column:entity_type;index" json:"entity_type,omitempty"`
	Kind					string						`gorm:"column:kind;not null;index" json:"kind"`
	Status				string						`gorm:"gorm:column:status;not null;index" json:"status"`
	Stage					string						`gorm:"column:stage;not null;index" json:"stage"`
	Progress			int								`gorm:"column:progress;not null" json:"progress"`
	Message				string						`gorm:"column:message;type:text" json:"message,omitempty"`
	Data					datatypes.JSON		`gorm:"type:jsonb;column:data" json:"data,omitempty"`
	CreatedAt			time.Time					`gorm:"not null;default:now();index" json:"created_at"`
	DeletedAt			gorm.DeletedAt		`gorm:"index" json:"deleted_at,omitempty"`
}

func (JobRunEvent) TableName() string { return "job_run_event" }










