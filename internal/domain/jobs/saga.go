package jobs

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

// SagaRun is the durable, canonical saga ledger header row.
// It links a root job to a sequence of compensating actions for external side effects.
type SagaRun struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	OwnerUserID uuid.UUID `gorm:"type:uuid;not null;index" json:"owner_user_id"`
	RootJobID   uuid.UUID `gorm:"type:uuid;not null;uniqueIndex" json:"root_job_id"`

	// running|succeeded|failed|compensating|compensated
	Status string `gorm:"column:status;not null;index" json:"status"`

	CreatedAt time.Time `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time `gorm:"not null;default:now();index" json:"updated_at"`
}

func (SagaRun) TableName() string { return "saga_run" }

// SagaAction is a durable compensation record for an external side effect.
// Every stage must append actions inside the same DB transaction that commits canonical state.
type SagaAction struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	SagaID uuid.UUID `gorm:"type:uuid;not null;index:idx_saga_action_saga_seq,unique,priority:1;index" json:"saga_id"`
	Seq    int64     `gorm:"column:seq;type:bigint;not null;index:idx_saga_action_saga_seq,unique,priority:2;index" json:"seq"`

	// gcs_delete_key|gcs_delete_prefix|vector_delete_ids|pinecone_delete_ids(legacy)|...
	Kind string `gorm:"column:kind;not null;index" json:"kind"`

	Payload datatypes.JSON `gorm:"column:payload;type:jsonb" json:"payload"`

	// pending|done|failed
	Status string `gorm:"column:status;not null;index" json:"status"`

	CreatedAt time.Time `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time `gorm:"not null;default:now();index" json:"updated_at"`
}

func (SagaAction) TableName() string { return "saga_action" }
