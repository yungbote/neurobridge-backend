package joins

import (
	"time"

	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/domain/learning/core"
	"gorm.io/gorm"
)

type PathNodeActivity struct {
	ID uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`

	PathNodeID uuid.UUID      `gorm:"type:uuid;not null;index:idx_path_node_activity,unique,priority:1" json:"path_node_id"`
	PathNode   *core.PathNode `gorm:"constraint:OnDelete:CASCADE;foreignKey:PathNodeID;references:ID" json:"path_node,omitempty"`

	ActivityID uuid.UUID      `gorm:"type:uuid;not null;index:idx_path_node_activity,unique,priority:2" json:"activity_id"`
	Activity   *core.Activity `gorm:"constraint:OnDelete:CASCADE;foreignKey:ActivityID;references:ID" json:"activity,omitempty"`

	Rank      int  `gorm:"column:rank;not null;default:0" json:"rank"`
	IsPrimary bool `gorm:"column:is_primary;not null;default:true" json:"is_primary"`

	CreatedAt time.Time      `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (PathNodeActivity) TableName() string { return "path_node_activity" }
