package products

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

// DocConstraintReport stores the constraint validation results for a doc variant.
type DocConstraintReport struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	ReportID  string     `gorm:"column:report_id;type:text;not null;uniqueIndex" json:"report_id"`
	VariantID *uuid.UUID `gorm:"type:uuid;column:variant_id;index" json:"variant_id,omitempty"`
	TraceID   string     `gorm:"column:trace_id;type:text;index" json:"trace_id,omitempty"`

	SchemaVersion  int  `gorm:"column:schema_version;not null" json:"schema_version"`
	Passed         bool `gorm:"column:passed;not null;index" json:"passed"`
	ViolationCount int  `gorm:"column:violation_count;not null" json:"violation_count"`

	ReportJSON datatypes.JSON `gorm:"type:jsonb;column:report_json;not null" json:"report_json"`

	CreatedAt time.Time `gorm:"not null;default:now();index" json:"created_at"`
}

func (DocConstraintReport) TableName() string { return "doc_constraint_report" }
