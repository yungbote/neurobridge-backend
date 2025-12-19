package products

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ChainPrior stores population-level priors at the chain+representation level.
// This is how “similar chains tend to work best with X representation” becomes learnable.
type ChainPrior struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	ChainKey string `gorm:"column:chain_key;not null;index:idx_chain_prior,unique,priority:1" json:"chain_key"`

	// Optional explicit cohort segment key (behavioral segment, NOT demographics).
	CohortKey string `gorm:"column:cohort_key;not null;default:'';index:idx_chain_prior,unique,priority:2" json:"cohort_key"`

	ActivityKind string `gorm:"column:activity_kind;not null;index:idx_chain_prior,unique,priority:3" json:"activity_kind"`
	Modality     string `gorm:"column:modality;not null;index:idx_chain_prior,unique,priority:4" json:"modality"`
	Variant      string `gorm:"column:variant;not null;index:idx_chain_prior,unique,priority:5" json:"variant"`

	// RepresentationKey is a deterministic hash over the representation tuple.
	RepresentationKey string `gorm:"column:representation_key;not null;index:idx_chain_prior,unique,priority:6" json:"representation_key"`

	EMA float64 `gorm:"column:ema;not null;default:0" json:"ema"`
	N   int     `gorm:"column:n;not null;default:0" json:"n"`

	// Beta params for binary feedback (liked/worked)
	A float64 `gorm:"column:a;not null;default:1" json:"a"`
	B float64 `gorm:"column:b;not null;default:1" json:"b"`

	CompletionRate float64 `gorm:"column:completion_rate;not null;default:0" json:"completion_rate"`
	MedianDwellMS  int     `gorm:"column:median_dwell_ms;not null;default:0" json:"median_dwell_ms"`

	LastObservedAt *time.Time `gorm:"column:last_observed_at;index" json:"last_observed_at,omitempty"`

	UpdatedAt time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (ChainPrior) TableName() string { return "chain_prior" }










