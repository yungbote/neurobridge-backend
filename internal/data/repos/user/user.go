package user

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type UserRepo interface {
	Create(dbc dbctx.Context, users []*types.User) ([]*types.User, error)
	GetByIDs(dbc dbctx.Context, userIDs []uuid.UUID) ([]*types.User, error)
	GetByEmails(dbc dbctx.Context, userEmails []string) ([]*types.User, error)
	EmailExists(dbc dbctx.Context, userEmail string) (bool, error)
	UpdateName(dbc dbctx.Context, userID uuid.UUID, firstName, lastName string) error
	UpdatePreferredTheme(dbc dbctx.Context, userID uuid.UUID, preferredTheme string) error
	UpdateAvatarColor(dbc dbctx.Context, userID uuid.UUID, avatarColor string) error
	UpdateAvatarFields(dbc dbctx.Context, userID uuid.UUID, bucketKey, avatarURL string) error
}

type userRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewUserRepo(db *gorm.DB, baseLog *logger.Logger) UserRepo {
	repoLog := baseLog.With("repo", "UserRepo")
	return &userRepo{db: db, log: repoLog}
}

func (ur *userRepo) Create(dbc dbctx.Context, users []*types.User) ([]*types.User, error) {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = ur.db
	}

	if len(users) == 0 {
		return []*types.User{}, nil
	}

	if err := transaction.WithContext(dbc.Ctx).Create(&users).Error; err != nil {
		return nil, err
	}

	return users, nil
}

func (ur *userRepo) GetByIDs(dbc dbctx.Context, userIDs []uuid.UUID) ([]*types.User, error) {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = ur.db
	}

	var results []*types.User

	if len(userIDs) == 0 {
		return results, nil
	}

	if err := transaction.WithContext(dbc.Ctx).
		Where("id IN ?", userIDs).
		Find(&results).Error; err != nil {
		return nil, err
	}
	return results, nil
}

func (ur *userRepo) GetByEmails(dbc dbctx.Context, userEmails []string) ([]*types.User, error) {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = ur.db
	}

	var results []*types.User
	if len(userEmails) == 0 {
		return results, nil
	}

	if err := transaction.WithContext(dbc.Ctx).
		Where("email IN ?", userEmails).
		Find(&results).Error; err != nil {
		return nil, err
	}
	return results, nil
}

func (ur *userRepo) EmailExists(dbc dbctx.Context, userEmail string) (bool, error) {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = ur.db
	}

	var count int64

	if err := transaction.WithContext(dbc.Ctx).
		Model(&types.User{}).
		Where("email = ?", userEmail).
		Count(&count).Error; err != nil {
		return false, err
	}
	exists := count > 0
	return exists, nil
}

func (ur *userRepo) UpdateName(dbc dbctx.Context, userID uuid.UUID, firstName, lastName string) error {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = ur.db
	}
	return transaction.WithContext(dbc.Ctx).
		Model(&types.User{}).
		Where("id = ?", userID).
		Updates(map[string]any{
			"first_name": firstName,
			"last_name":  lastName,
		}).Error
}

func (ur *userRepo) UpdatePreferredTheme(dbc dbctx.Context, userID uuid.UUID, preferredTheme string) error {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = ur.db
	}
	return transaction.WithContext(dbc.Ctx).
		Model(&types.User{}).
		Where("id = ?", userID).
		Update("preferred_theme", preferredTheme).Error
}

func (ur *userRepo) UpdateAvatarColor(dbc dbctx.Context, userID uuid.UUID, avatarColor string) error {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = ur.db
	}
	return transaction.WithContext(dbc.Ctx).
		Model(&types.User{}).
		Where("id = ?", userID).
		Update("avatar_color", avatarColor).Error
}

func (ur *userRepo) UpdateAvatarFields(dbc dbctx.Context, userID uuid.UUID, bucketKey, avatarURL string) error {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = ur.db
	}
	return transaction.WithContext(dbc.Ctx).
		Model(&types.User{}).
		Where("id = ?", userID).
		Updates(map[string]any{
			"avatar_bucket_key": bucketKey,
			"avatar_url":        avatarURL,
		}).Error
}
