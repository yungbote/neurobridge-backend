package products

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// StructuralDriftMetric stores aggregate drift metrics for a graph version over a time window.
type StructuralDriftMetric struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	GraphVersion string `gorm:"column:graph_version;type:text;index" json:"graph_version,omitempty"`

	MetricName  string    `gorm:"column:metric_name;type:text;not null;index" json:"metric_name"`
	WindowStart time.Time `gorm:"column:window_start;not null;index" json:"window_start"`
	WindowEnd   time.Time `gorm:"column:window_end;not null;index" json:"window_end"`

	Value     float64 `gorm:"column:value;not null;default:0" json:"value"`
	Threshold float64 `gorm:"column:threshold;not null;default:0" json:"threshold"`
	Status    string  `gorm:"column:status;type:text;not null;default:'ok';index" json:"status"`

	Metadata datatypes.JSON `gorm:"column:metadata;type:jsonb" json:"metadata,omitempty"`

	CreatedAt time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (StructuralDriftMetric) TableName() string { return "structural_drift_metric" }
