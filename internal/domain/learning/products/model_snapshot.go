package products

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// ModelSnapshot stores offline-trained policy/model versions for runtime selection.
type ModelSnapshot struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	ModelKey string `gorm:"column:model_key;not null;index:idx_model_snapshot,unique,priority:1" json:"model_key"`
	Version  int    `gorm:"column:version;not null;index:idx_model_snapshot,unique,priority:2" json:"version"`
	Active   bool   `gorm:"column:active;not null;default:false;index" json:"active"`

	ParamsJSON  datatypes.JSON `gorm:"column:params_json;type:jsonb" json:"params_json"`
	MetricsJSON datatypes.JSON `gorm:"column:metrics_json;type:jsonb" json:"metrics_json"`

	CreatedAt time.Time      `gorm:"not null;default:now();index" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (ModelSnapshot) TableName() string { return "model_snapshot" }
