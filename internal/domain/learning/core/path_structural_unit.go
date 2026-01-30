package core

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// PathStructuralUnit represents ordered/branched pedagogical structure in a path.
type PathStructuralUnit struct {
	ID     uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	PathID uuid.UUID `gorm:"type:uuid;not null;index;index:idx_path_psu,priority:1;uniqueIndex:idx_path_psu_key,priority:1" json:"path_id"`

	PatternKind string `gorm:"column:pattern_kind;not null;index" json:"pattern_kind"`
	PsuKey      string `gorm:"column:psu_key;not null;index:idx_path_psu,priority:2;uniqueIndex:idx_path_psu_key,priority:2" json:"psu_key"`

	MemberNodeIDs datatypes.JSON `gorm:"column:member_node_ids;type:jsonb" json:"member_node_ids"`
	StructureEnc  string         `gorm:"column:structure_enc;type:text" json:"structure_enc"`

	DerivedCanonicalConceptIDs datatypes.JSON `gorm:"column:derived_canonical_concept_ids;type:jsonb" json:"derived_canonical_concept_ids,omitempty"`

	ChainSignatureID *uuid.UUID     `gorm:"type:uuid;column:chain_signature_id;index" json:"chain_signature_id,omitempty"`
	LocalRole        string         `gorm:"column:local_role;type:text" json:"local_role,omitempty"`
	EvidenceState    datatypes.JSON `gorm:"column:evidence_state;type:jsonb" json:"evidence_state,omitempty"`

	CreatedAt time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (PathStructuralUnit) TableName() string { return "path_structural_unit" }
