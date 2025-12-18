package learning

import (
	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/domain/materials"
	"github.com/yungbote/neurobridge-backend/internal/domain/user"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"time"
)

type CourseBlueprint struct {
	ID            uuid.UUID              `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	MaterialSetID uuid.UUID              `gorm:"type:uuid;not null;index" json:"material_set_id"`
	MaterialSet   *materials.MaterialSet `gorm:"constraint:OnDelete:CASCADE;foreignKey:MaterialSetID;references:ID" json:"material_set,omitempty"`
	UserID        uuid.UUID              `gorm:"type:uuid;not null;index" json:"user_id"`
	User          *user.User             `gorm:"constraint:OnDelete:CASCADE;foreignKey:UserID;references:ID" json:"user,omitempty"`
	BlueprintJSON datatypes.JSON         `gorm:"column:blueprint_json;type:jsonb;not null" json:"blueprint_json"`
	CreatedAt     time.Time              `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt     time.Time              `gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt     gorm.DeletedAt         `gorm:"index" json:"deleted_at,omitempty"`
}

func (CourseBlueprint) TableName() string { return "course_blueprint" }
