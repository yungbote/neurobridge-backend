package products

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

// LearningNodeFigure stores a planned (and optionally rendered) raster figure for a PathNode.
// Figure planning and rendering are separated from NodeDoc generation so NodeDoc only references
// already-available assets.
type LearningNodeFigure struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	UserID     uuid.UUID `gorm:"type:uuid;not null;index:idx_learning_node_figure_key,unique,priority:1;index" json:"user_id"`
	PathID     uuid.UUID `gorm:"type:uuid;not null;index" json:"path_id"`
	PathNodeID uuid.UUID `gorm:"type:uuid;not null;index:idx_learning_node_figure_key,unique,priority:2;index" json:"path_node_id"`
	Slot       int       `gorm:"column:slot;not null;index:idx_learning_node_figure_key,unique,priority:3" json:"slot"`

	SchemaVersion int            `gorm:"column:schema_version;not null" json:"schema_version"`
	PlanJSON      datatypes.JSON `gorm:"type:jsonb;column:plan_json;not null" json:"plan_json"`

	PromptHash  string `gorm:"column:prompt_hash;type:text;not null;index" json:"prompt_hash"`
	SourcesHash string `gorm:"column:sources_hash;type:text;not null;index" json:"sources_hash"`

	Status string `gorm:"column:status;type:text;not null;index" json:"status"` // planned|rendered|failed|skipped

	// Filled after rendering.
	AssetID         *uuid.UUID `gorm:"type:uuid;column:asset_id;index" json:"asset_id,omitempty"`
	AssetStorageKey string     `gorm:"column:asset_storage_key;type:text" json:"asset_storage_key,omitempty"`
	AssetURL        string     `gorm:"column:asset_url;type:text" json:"asset_url,omitempty"`
	AssetMimeType   string     `gorm:"column:asset_mime_type;type:text" json:"asset_mime_type,omitempty"`

	Error string `gorm:"column:error;type:text" json:"error,omitempty"`

	CreatedAt time.Time `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time `gorm:"not null;default:now();index" json:"updated_at"`
}

func (LearningNodeFigure) TableName() string { return "learning_node_figure" }

