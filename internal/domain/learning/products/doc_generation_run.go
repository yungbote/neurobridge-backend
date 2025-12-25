package products

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

// LearningDocGenerationRun records attempts and quality/validation signals for generated artifacts.
// This is used for observability and debugging; it is not part of the learner-facing product.
type LearningDocGenerationRun struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	ArtifactType string     `gorm:"column:artifact_type;type:text;not null;index" json:"artifact_type"`
	ArtifactID   *uuid.UUID `gorm:"type:uuid;column:artifact_id;index" json:"artifact_id,omitempty"`

	UserID     uuid.UUID `gorm:"type:uuid;not null;index" json:"user_id"`
	PathID     uuid.UUID `gorm:"type:uuid;not null;index" json:"path_id"`
	PathNodeID uuid.UUID `gorm:"type:uuid;not null;index" json:"path_node_id"`

	Status        string `gorm:"column:status;type:text;not null;index" json:"status"`
	Model         string `gorm:"column:model;type:text;not null" json:"model"`
	PromptVersion string `gorm:"column:prompt_version;type:text;not null" json:"prompt_version"`
	Attempt       int    `gorm:"column:attempt;not null" json:"attempt"`

	LatencyMS int `gorm:"column:latency_ms;not null" json:"latency_ms"`
	TokensIn  int `gorm:"column:tokens_in;not null" json:"tokens_in"`
	TokensOut int `gorm:"column:tokens_out;not null" json:"tokens_out"`

	ValidationErrors datatypes.JSON `gorm:"type:jsonb;column:validation_errors" json:"validation_errors,omitempty"`
	QualityMetrics   datatypes.JSON `gorm:"type:jsonb;column:quality_metrics" json:"quality_metrics,omitempty"`

	CreatedAt time.Time `gorm:"not null;default:now();index" json:"created_at"`
}

func (LearningDocGenerationRun) TableName() string { return "learning_doc_generation_run" }
