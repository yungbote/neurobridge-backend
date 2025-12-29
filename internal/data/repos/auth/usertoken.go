package auth

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type UserTokenRepo interface {
	Create(dbc dbctx.Context, userTokens []*types.UserToken) ([]*types.UserToken, error)
	GetByIDs(dbc dbctx.Context, tokenIDs []uuid.UUID) ([]*types.UserToken, error)
	GetByUsers(dbc dbctx.Context, users []*types.User) ([]*types.UserToken, error)
	GetByUserIDs(dbc dbctx.Context, userIDs []uuid.UUID) ([]*types.UserToken, error)
	GetByAccessTokens(dbc dbctx.Context, accessTokens []string) ([]*types.UserToken, error)
	GetByRefreshTokens(dbc dbctx.Context, refreshTokens []string) ([]*types.UserToken, error)
	SoftDeleteByTokens(dbc dbctx.Context, userTokens []*types.UserToken) error
	SoftDeleteByIDs(dbc dbctx.Context, tokenIDs []uuid.UUID) error
	SoftDeleteByUserIDs(dbc dbctx.Context, userIDs []uuid.UUID) error
	FullDeleteByTokens(dbc dbctx.Context, userTokens []*types.UserToken) error
	FullDeleteByIDs(dbc dbctx.Context, tokenIDs []uuid.UUID) error
	FullDeleteByUserIDs(dbc dbctx.Context, userIDs []uuid.UUID) error
}

type userTokenRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewUserTokenRepo(db *gorm.DB, baseLog *logger.Logger) UserTokenRepo {
	repoLog := baseLog.With("repo", "UserTokenRepo")
	return &userTokenRepo{db: db, log: repoLog}
}

func (utr *userTokenRepo) Create(dbc dbctx.Context, userTokens []*types.UserToken) ([]*types.UserToken, error) {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = utr.db
	}

	if len(userTokens) == 0 {
		return []*types.UserToken{}, nil
	}

	if err := transaction.WithContext(dbc.Ctx).Create(&userTokens).Error; err != nil {
		return nil, err
	}

	return userTokens, nil
}

func (utr *userTokenRepo) GetByIDs(dbc dbctx.Context, tokenIDs []uuid.UUID) ([]*types.UserToken, error) {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = utr.db
	}

	var results []*types.UserToken

	if len(tokenIDs) == 0 {
		return results, nil
	}

	if err := transaction.WithContext(dbc.Ctx).
		Where("id IN ?", tokenIDs).
		Find(&results).Error; err != nil {
		return nil, err
	}

	return results, nil
}

func (utr *userTokenRepo) GetByUsers(dbc dbctx.Context, users []*types.User) ([]*types.UserToken, error) {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = utr.db
	}

	if len(users) == 0 {
		return []*types.UserToken{}, nil
	}

	var userIDs []uuid.UUID
	for _, u := range users {
		userIDs = append(userIDs, u.ID)
	}

	return utr.GetByUserIDs(dbctx.Context{Ctx: dbc.Ctx, Tx: transaction}, userIDs)
}

func (utr *userTokenRepo) GetByUserIDs(dbc dbctx.Context, userIDs []uuid.UUID) ([]*types.UserToken, error) {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = utr.db
	}

	var results []*types.UserToken

	if len(userIDs) == 0 {
		return results, nil
	}

	if err := transaction.WithContext(dbc.Ctx).
		Where("user_id IN ?", userIDs).
		Find(&results).Error; err != nil {
		return nil, err
	}

	return results, nil
}

func (utr *userTokenRepo) GetByAccessTokens(dbc dbctx.Context, accessTokens []string) ([]*types.UserToken, error) {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = utr.db
	}

	var results []*types.UserToken

	if len(accessTokens) == 0 {
		return results, nil
	}

	if err := transaction.WithContext(dbc.Ctx).
		Where("access_token IN ?", accessTokens).
		Find(&results).Error; err != nil {
		return nil, err
	}

	return results, nil
}

func (utr *userTokenRepo) GetByRefreshTokens(dbc dbctx.Context, refreshTokens []string) ([]*types.UserToken, error) {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = utr.db
	}

	var results []*types.UserToken

	if len(refreshTokens) == 0 {
		return results, nil
	}

	if err := transaction.WithContext(dbc.Ctx).
		Where("refresh_token IN ?", refreshTokens).
		Find(&results).Error; err != nil {
		return nil, err
	}

	return results, nil
}

func (utr *userTokenRepo) SoftDeleteByTokens(dbc dbctx.Context, userTokens []*types.UserToken) error {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = utr.db
	}

	if len(userTokens) == 0 {
		return nil
	}

	var tokenIDs []uuid.UUID
	for _, t := range userTokens {
		tokenIDs = append(tokenIDs, t.ID)
	}

	return utr.SoftDeleteByIDs(dbctx.Context{Ctx: dbc.Ctx, Tx: transaction}, tokenIDs)
}

func (utr *userTokenRepo) SoftDeleteByIDs(dbc dbctx.Context, tokenIDs []uuid.UUID) error {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = utr.db
	}

	if len(tokenIDs) == 0 {
		return nil
	}

	if err := transaction.WithContext(dbc.Ctx).
		Where("id IN (?)", tokenIDs).
		Delete(&types.UserToken{}).Error; err != nil {
		return err
	}

	return nil
}

func (utr *userTokenRepo) SoftDeleteByUserIDs(dbc dbctx.Context, userIDs []uuid.UUID) error {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = utr.db
	}

	if len(userIDs) == 0 {
		return nil
	}

	if err := transaction.WithContext(dbc.Ctx).
		Where("user_id IN (?)", userIDs).
		Delete(&types.UserToken{}).Error; err != nil {
		return err
	}

	return nil
}

func (utr *userTokenRepo) FullDeleteByTokens(dbc dbctx.Context, userTokens []*types.UserToken) error {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = utr.db
	}

	if len(userTokens) == 0 {
		return nil
	}

	var tokenIDs []uuid.UUID
	for _, t := range userTokens {
		tokenIDs = append(tokenIDs, t.ID)
	}

	return utr.FullDeleteByIDs(dbctx.Context{Ctx: dbc.Ctx, Tx: transaction}, tokenIDs)
}

func (utr *userTokenRepo) FullDeleteByIDs(dbc dbctx.Context, tokenIDs []uuid.UUID) error {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = utr.db
	}

	if len(tokenIDs) == 0 {
		return nil
	}

	if err := transaction.WithContext(dbc.Ctx).
		Unscoped().
		Where("id IN (?)", tokenIDs).
		Delete(&types.UserToken{}).Error; err != nil {
		return err
	}

	return nil
}

func (utr *userTokenRepo) FullDeleteByUserIDs(dbc dbctx.Context, userIDs []uuid.UUID) error {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = utr.db
	}

	if len(userIDs) == 0 {
		return nil
	}

	if err := transaction.WithContext(dbc.Ctx).
		Unscoped().
		Where("user_id IN (?)", userIDs).
		Delete(&types.UserToken{}).Error; err != nil {
		return err
	}

	return nil
}
