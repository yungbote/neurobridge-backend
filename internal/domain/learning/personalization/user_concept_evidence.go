package personalization

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// UserConceptEvidence stores an immutable audit log row for mastery/confidence updates.
// Each (user, concept, source, source_ref) is unique for idempotent writes.
type UserConceptEvidence struct {
	ID        uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	UserID    uuid.UUID `gorm:"type:uuid;not null;index:idx_user_concept_evidence,priority:1;uniqueIndex:idx_user_concept_evidence_key,priority:1" json:"user_id"`
	ConceptID uuid.UUID `gorm:"type:uuid;not null;index:idx_user_concept_evidence,priority:2;uniqueIndex:idx_user_concept_evidence_key,priority:2" json:"concept_id"`

	Source    string `gorm:"column:source;type:text;not null;index;uniqueIndex:idx_user_concept_evidence_key,priority:3" json:"source"`
	SourceRef string `gorm:"column:source_ref;type:text;not null;uniqueIndex:idx_user_concept_evidence_key,priority:4" json:"source_ref"`

	EventID   *uuid.UUID `gorm:"type:uuid;column:event_id;index" json:"event_id,omitempty"`
	EventType string     `gorm:"column:event_type;type:text" json:"event_type,omitempty"`

	OccurredAt time.Time `gorm:"column:occurred_at;index" json:"occurred_at"`

	PriorMastery     float64 `gorm:"column:prior_mastery;not null;default:0" json:"prior_mastery"`
	PriorConfidence  float64 `gorm:"column:prior_confidence;not null;default:0" json:"prior_confidence"`
	PostMastery      float64 `gorm:"column:post_mastery;not null;default:0" json:"post_mastery"`
	PostConfidence   float64 `gorm:"column:post_confidence;not null;default:0" json:"post_confidence"`
	MasteryDelta     float64 `gorm:"column:mastery_delta;not null;default:0" json:"mastery_delta"`
	ConfidenceDelta  float64 `gorm:"column:confidence_delta;not null;default:0" json:"confidence_delta"`
	SignalStrength   float64 `gorm:"column:signal_strength;not null;default:0" json:"signal_strength"`
	SignalConfidence float64 `gorm:"column:signal_confidence;not null;default:0" json:"signal_confidence"`

	Payload datatypes.JSON `gorm:"column:payload;type:jsonb" json:"payload,omitempty"`

	CreatedAt time.Time      `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (UserConceptEvidence) TableName() string { return "user_concept_evidence" }
