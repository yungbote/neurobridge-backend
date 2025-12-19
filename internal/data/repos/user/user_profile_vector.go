package user

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type UserProfileVectorRepo interface {
	GetByUserID(ctx context.Context, tx *gorm.DB, userID uuid.UUID) (*types.UserProfileVector, error)
	Upsert(ctx context.Context, tx *gorm.DB, row *types.UserProfileVector) error
}

type userProfileVectorRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewUserProfileVectorRepo(db *gorm.DB, baseLog *logger.Logger) UserProfileVectorRepo {
	return &userProfileVectorRepo{db: db, log: baseLog.With("repo", "UserProfileVectorRepo")}
}

func (r *userProfileVectorRepo) GetByUserID(ctx context.Context, tx *gorm.DB, userID uuid.UUID) (*types.UserProfileVector, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	if userID == uuid.Nil {
		return nil, nil
	}
	var row types.UserProfileVector
	if err := t.WithContext(ctx).Where("user_id = ?", userID).Limit(1).Find(&row).Error; err != nil {
		return nil, err
	}
	if row.ID == uuid.Nil {
		return nil, nil
	}
	return &row, nil
}

func (r *userProfileVectorRepo) Upsert(ctx context.Context, tx *gorm.DB, row *types.UserProfileVector) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if row == nil || row.UserID == uuid.Nil {
		return nil
	}
	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	row.UpdatedAt = time.Now().UTC()
	return t.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "user_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"profile_doc",
				"embedding",
				"vector_id",
				"updated_at",
			}),
		}).
		Create(row).Error
}
