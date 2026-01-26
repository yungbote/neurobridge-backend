package materials

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// MaterialChunkSignal stores chunk-level signal metadata relative to a material's intent.
type MaterialChunkSignal struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	MaterialChunkID uuid.UUID      `gorm:"type:uuid;not null;index;uniqueIndex:idx_material_chunk_signal" json:"material_chunk_id"`
	MaterialChunk   *MaterialChunk `gorm:"constraint:OnDelete:CASCADE;foreignKey:MaterialChunkID;references:ID" json:"material_chunk,omitempty"`
	MaterialFileID  uuid.UUID      `gorm:"type:uuid;not null;index" json:"material_file_id"`
	MaterialSetID   uuid.UUID      `gorm:"type:uuid;not null;index" json:"material_set_id"`

	Role                 string  `gorm:"type:text;not null;default:'';index" json:"role"`
	SignalStrength       float64 `gorm:"type:double precision;not null;default:0" json:"signal_strength"`
	FloorSignal          float64 `gorm:"type:double precision;not null;default:0" json:"floor_signal"`
	IntentAlignmentScore float64 `gorm:"type:double precision;not null;default:0" json:"intent_alignment_score"`
	SetPositionScore     float64 `gorm:"type:double precision;not null;default:0" json:"set_position_score"`
	NoveltyScore         float64 `gorm:"type:double precision;not null;default:0" json:"novelty_score"`
	DensityScore         float64 `gorm:"type:double precision;not null;default:0" json:"density_score"`
	ComplexityScore      float64 `gorm:"type:double precision;not null;default:0" json:"complexity_score"`
	LoadBearingScore     float64 `gorm:"type:double precision;not null;default:0" json:"load_bearing_score"`
	CompoundWeight       float64 `gorm:"type:double precision;not null;default:0" json:"compound_weight"`

	Trajectory datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'" json:"trajectory"`
	Metadata   datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'" json:"metadata"`

	CreatedAt time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (MaterialChunkSignal) TableName() string { return "material_chunk_signal" }
