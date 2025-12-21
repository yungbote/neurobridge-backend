package auth

import (
	"context"
	"gorm.io/gorm"
	"github.com/google/uuid"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
)

type UserIdentityRepo interface {
	Create(ctx context.Context, tx *gorm.DB, ids []*types.UserIdentity) ([]*types.UserIdentity, error)
	GetByProviderSubs(ctx context.Context, tx *gorm.DB, provider string, subs []string) ([]*types.UserIdentity, error)
	GetByUserIDs(ctx context.Context, tx *gorm.DB, userIDs []uuid.UUID) ([]*types.UserIdentity, error)
}

type userIdentityRepo struct {
	db					*gorm.DB
	log					*logger.Logger
}

func NewUserIdentityRepo(db *gorm.DB, baseLog *logger.Logger) UserIdentityRepo {
	return &userIdentityRepo{
		db:				db,
		log:			baseLog.With("repo", "UserIdentityRepo"),
	}
}

func (r *userIdentityRepo) Create(ctx context.Context, tx *gorm.DB, ids []*types.UserIdentity) ([]*types.UserIdentity, error) {
	txx := tx
	if txx == nil {
		txx = r.db
	}
	if err := txx.WithContext(ctx).Create(&ids).Error; err != nil {
		return nil, err
	}
	return ids, nil
}

func (r *userIdentityRepo) GetByProviderSubs(ctx context.Context, tx *gorm.DB, provider string, subs []string) ([]*types.UserIdentity, error) {
	txx := tx
	if txx == nil {
		txx = r.db
	}
	var out []*types.UserIdentity
	if err := txx.WithContext(ctx).Where("provider = ? AND provider_sub IN ?", provider, subs).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *userIdentityRepo) GetByUserIDs(ctx context.Context, tx *gorm.DB, userIDs []uuid.UUID) ([]*types.UserIdentity, error) {
	txx := tx
	if txx == nil {
		txx = r.db
	}
	var out []*types.UserIdentity
	if err := txx.WithContext(ctx).Where("user_id IN ?", userIDs).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}










