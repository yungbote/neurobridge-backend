package auth

import (
	"time"
	"gorm.io/gorm"
	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/domain/user"
)

type UserIdentity struct {
	ID						uuid.UUID			`gorm:"type:uuid;default:uuid_generate_v4();primaryKey" json:"id"`
	UserID				uuid.UUID			`gorm:"index;not null" json:"user_id"`
	User					*user.User	  `gorm:"constraint:OnDelete:CASCADE;foreignKey:UserID;references:ID" json:"user,omitempty"`
	Provider			string				`gorm:"not null;column:provider;uniqueIndex:idx_user_identity_provider_sub,priority:1" json:"provider"`
	ProviderSub		string				`gorm:"not null;column:provider_sub;uniqueIndex:idx_user_identity_provider_sub,priority:2" json:"provider_sub"`
	Email					string				`gorm:"column:email" json:"email"`
	EmailVerified bool					`gorm:"not null;default:false;column:email_verified" json:"email_verified"`
	CreatedAt			time.Time			`gorm:"not null;default:now()" json:"created_at"`
	UpdatedAt			time.Time			`gorm:"not null;default:now()" json:"updated_at"`
	DeletedAt			gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

func (UserIdentity) TableName() string { return "user_identity" }










