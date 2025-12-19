package products

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Cohort priors (population truths).
type CohortPrior struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	ConceptID        *uuid.UUID `gorm:"type:uuid;index:idx_cohort_prior,unique,priority:1" json:"concept_id,omitempty"`
	ConceptClusterID *uuid.UUID `gorm:"type:uuid;index:idx_cohort_prior,unique,priority:2" json:"concept_cluster_id,omitempty"`

	ActivityKind string `gorm:"column:activity_kind;not null;index:idx_cohort_prior,unique,priority:3" json:"activity_kind"`
	Modality     string `gorm:"column:modality;not null;index:idx_cohort_prior,unique,priority:4" json:"modality"`
	Variant      string `gorm:"column:variant;not null;index:idx_cohort_prior,unique,priority:5" json:"variant"`

	EMA float64 `gorm:"column:ema;not null;default:0" json:"ema"`
	N   int     `gorm:"column:n;not null;default:0" json:"n"`
	A   float64 `gorm:"column:a;not null;default:1" json:"a"`
	B   float64 `gorm:"column:b;not null;default:1" json:"b"`

	CompletionRate float64 `gorm:"column:completion_rate;not null;default:0" json:"completion_rate"`
	MedianDwellMS  int     `gorm:"column:median_dwell_ms;not null;default:0" json:"median_dwell_ms"`

	LastObservedAt *time.Time `gorm:"column:last_observed_at;index" json:"last_observed_at,omitempty"`

	UpdatedAt time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (CohortPrior) TableName() string { return "cohort_prior" }
