package user

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type User struct {
	ID              uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	Email           string    `gorm:"uniqueIndex;not null;column:email" json:"email"`
	Password        string    `gorm:"not null;column:password" json:"-"`
	FirstName       string    `gorm:"not null;column:first_name" json:"first_name"`
	LastName        string    `gorm:"not null;column:last_name" json:"last_name"`
	AvatarBucketKey string    `gorm:"column:avatar_bucket_key" json:"avatar_bucket_key"`
	AvatarURL       string    `gorm:"column:avatar_url" json:"avatar_url"`
	AvatarColor     string    `gorm:"column:avatar_color" json:"avatar_color"`

	PreferredTheme string `gorm:"column:preferred_theme" json:"preferred_theme"`

	CreatedAt time.Time      `gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt time.Time      `gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (User) TableName() string { return "user" }

// --- User profile vector (retrieval) ---

type UserProfileVector struct {
	ID     uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	UserID uuid.UUID `gorm:"type:uuid;not null;uniqueIndex" json:"user_id"`

	ProfileDoc string         `gorm:"column:profile_doc;type:text" json:"profile_doc"`
	Embedding  datatypes.JSON `gorm:"column:embedding;type:jsonb" json:"embedding"` // []float32
	VectorID   string         `gorm:"column:vector_id;index" json:"vector_id,omitempty"`

	UpdatedAt time.Time      `gorm:"not null;default:now();index" json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (UserProfileVector) TableName() string { return "user_profile_vector" }
