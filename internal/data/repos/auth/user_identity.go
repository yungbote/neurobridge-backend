package auth

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type UserIdentityRepo interface {
	Create(dbc dbctx.Context, ids []*types.UserIdentity) ([]*types.UserIdentity, error)
	GetByProviderSubs(dbc dbctx.Context, provider string, subs []string) ([]*types.UserIdentity, error)
	GetByUserIDs(dbc dbctx.Context, userIDs []uuid.UUID) ([]*types.UserIdentity, error)
}

type userIdentityRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewUserIdentityRepo(db *gorm.DB, baseLog *logger.Logger) UserIdentityRepo {
	return &userIdentityRepo{
		db:  db,
		log: baseLog.With("repo", "UserIdentityRepo"),
	}
}

func (r *userIdentityRepo) Create(dbc dbctx.Context, ids []*types.UserIdentity) ([]*types.UserIdentity, error) {
	txx := dbc.Tx
	if txx == nil {
		txx = r.db
	}
	if err := txx.WithContext(dbc.Ctx).Create(&ids).Error; err != nil {
		return nil, err
	}
	return ids, nil
}

func (r *userIdentityRepo) GetByProviderSubs(dbc dbctx.Context, provider string, subs []string) ([]*types.UserIdentity, error) {
	txx := dbc.Tx
	if txx == nil {
		txx = r.db
	}
	var out []*types.UserIdentity
	if err := txx.WithContext(dbc.Ctx).Where("provider = ? AND provider_sub IN ?", provider, subs).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *userIdentityRepo) GetByUserIDs(dbc dbctx.Context, userIDs []uuid.UUID) ([]*types.UserIdentity, error) {
	txx := dbc.Tx
	if txx == nil {
		txx = r.db
	}
	var out []*types.UserIdentity
	if err := txx.WithContext(dbc.Ctx).Where("user_id IN ?", userIDs).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}
