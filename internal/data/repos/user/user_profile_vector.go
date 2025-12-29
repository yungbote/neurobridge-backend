package user

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type UserProfileVectorRepo interface {
	GetByUserID(dbc dbctx.Context, userID uuid.UUID) (*types.UserProfileVector, error)
	Upsert(dbc dbctx.Context, row *types.UserProfileVector) error
}

type userProfileVectorRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewUserProfileVectorRepo(db *gorm.DB, baseLog *logger.Logger) UserProfileVectorRepo {
	return &userProfileVectorRepo{db: db, log: baseLog.With("repo", "UserProfileVectorRepo")}
}

func (r *userProfileVectorRepo) GetByUserID(dbc dbctx.Context, userID uuid.UUID) (*types.UserProfileVector, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if userID == uuid.Nil {
		return nil, nil
	}
	var row types.UserProfileVector
	if err := t.WithContext(dbc.Ctx).Where("user_id = ?", userID).Limit(1).Find(&row).Error; err != nil {
		return nil, err
	}
	if row.ID == uuid.Nil {
		return nil, nil
	}
	return &row, nil
}

func (r *userProfileVectorRepo) Upsert(dbc dbctx.Context, row *types.UserProfileVector) error {
	t := dbc.Tx
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
	return t.WithContext(dbc.Ctx).
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
