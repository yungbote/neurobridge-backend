package personalization

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// UserConceptEdgeStat tracks per-user transfer safety metrics across concept edges.
type UserConceptEdgeStat struct {
	ID             uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	UserID         uuid.UUID `gorm:"type:uuid;not null;index:idx_user_concept_edge_stat,unique,priority:1" json:"user_id"`
	FromConceptID  uuid.UUID `gorm:"type:uuid;not null;index:idx_user_concept_edge_stat,unique,priority:2" json:"from_concept_id"`
	ToConceptID    uuid.UUID `gorm:"type:uuid;not null;index:idx_user_concept_edge_stat,unique,priority:3" json:"to_concept_id"`
	EdgeType       string    `gorm:"column:edge_type;not null;index:idx_user_concept_edge_stat,unique,priority:4" json:"edge_type"`

	Attempts       int        `gorm:"column:attempts;not null;default:0" json:"attempts"`
	FalseTransfers int        `gorm:"column:false_transfers;not null;default:0" json:"false_transfers"`
	ValidatedAt    *time.Time `gorm:"column:validated_at;index" json:"validated_at,omitempty"`

	LastTransferAt            *time.Time `gorm:"column:last_transfer_at;index" json:"last_transfer_at,omitempty"`
	LastValidationRequestedAt *time.Time `gorm:"column:last_validation_requested_at;index" json:"last_validation_requested_at,omitempty"`
	LastFalseAt               *time.Time `gorm:"column:last_false_at;index" json:"last_false_at,omitempty"`
	BlockedUntil              *time.Time `gorm:"column:blocked_until;index" json:"blocked_until,omitempty"`
	ThresholdBoost            float64    `gorm:"column:threshold_boost;not null;default:0" json:"threshold_boost"`

	CreatedAt time.Time      `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (UserConceptEdgeStat) TableName() string { return "user_concept_edge_stat" }
