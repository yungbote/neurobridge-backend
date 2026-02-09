package products

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

// DocRetrievalPack stores the evidence bundle used for doc generation.
type DocRetrievalPack struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	PathID     uuid.UUID `gorm:"type:uuid;not null;index" json:"path_id"`
	PathNodeID uuid.UUID `gorm:"type:uuid;not null;index:idx_doc_retrieval_pack,unique,priority:1" json:"path_node_id"`

	PackID           string `gorm:"column:pack_id;type:text;not null;index:idx_doc_retrieval_pack,unique,priority:2" json:"pack_id"`
	PolicyVersion    string `gorm:"column:policy_version;type:text;not null;index" json:"policy_version"`
	BlueprintVersion string `gorm:"column:blueprint_version;type:text;not null;index" json:"blueprint_version"`
	SchemaVersion    int    `gorm:"column:schema_version;not null" json:"schema_version"`

	PackJSON datatypes.JSON `gorm:"type:jsonb;column:pack_json;not null" json:"pack_json"`

	CreatedAt time.Time `gorm:"not null;default:now();index" json:"created_at"`
}

func (DocRetrievalPack) TableName() string { return "doc_retrieval_pack" }
