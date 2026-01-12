package user

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type UserPersonalizationPrefsRepo interface {
	GetByUserID(dbc dbctx.Context, userID uuid.UUID) (*types.UserPersonalizationPrefs, error)
	Upsert(dbc dbctx.Context, row *types.UserPersonalizationPrefs) error
}

type userPersonalizationPrefsRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewUserPersonalizationPrefsRepo(db *gorm.DB, baseLog *logger.Logger) UserPersonalizationPrefsRepo {
	return &userPersonalizationPrefsRepo{db: db, log: baseLog.With("repo", "UserPersonalizationPrefsRepo")}
}

func (r *userPersonalizationPrefsRepo) GetByUserID(dbc dbctx.Context, userID uuid.UUID) (*types.UserPersonalizationPrefs, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if userID == uuid.Nil {
		return nil, nil
	}
	var row types.UserPersonalizationPrefs
	if err := t.WithContext(dbc.Ctx).Where("user_id = ?", userID).Limit(1).Find(&row).Error; err != nil {
		return nil, err
	}
	if row.ID == uuid.Nil {
		return nil, nil
	}
	return &row, nil
}

func (r *userPersonalizationPrefsRepo) Upsert(dbc dbctx.Context, row *types.UserPersonalizationPrefs) error {
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
				"prefs_json",
				"updated_at",
			}),
		}).
		Create(row).Error
}
