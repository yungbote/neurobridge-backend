package auth

import (
	"context"
	"fmt"
	"time"
	"github.com/google/uuid"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"gorm.io/gorm"
)

type OAuthNonceRepo interface {
	Create(ctx context.Context, tx *gorm.DB, nonces []*types.OAuthNonce) ([]*types.OAuthNonce, error)
	GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.OAuthNonce, error)
	MarkUsed(ctx context.Context, tx *gorm.DB, id uuid.UUID) error
	FullDeleteExpires(ctx context.Context, tx *gorm.DB, before time.Time) error
}

type oauthNonceRepo struct {
	db				*gorm.DB
	log				*logger.Logger
}

func NewOAuthNonceRepo(db *gorm.DB, baseLog *logger.Logger) OAuthNonceRepo {
	repoLog := baseLog.With("repo", "OAuthNonceRepo")
	return &oauthNonceRepo{db: db, log: repoLog}
}

func (r *oauthNonceRepo) Create(ctx context.Context, tx *gorm.DB, nonces []*types.OAuthNonce) ([]*types.OAuthNonce, error) {
	txx := tx
	if txx == nil {
		txx = r.db
	}
	if len(nonces) == 0 {
		return []*types.OAuthNonce{}, nil
	}
	if err := txx.WithContext(ctx).Create(&nonces).Error; err != nil {
		return nil, err
	}
	return nonces, nil
}

func (r *oauthNonceRepo) GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.OAuthNonce, error) {
	txx := tx
	if txx == nil {
		txx = r.db
	}
	var results []*types.OAuthNonce
	if len(ids) == 0 { return results, nil }
	if err := txx.WithContext(ctx).Where("id IN ?", ids).Find(&results).Error; err != nil {
		return nil, err
	}
	return results, nil
}

func (r *oauthNonceRepo) MarkUsed(ctx context.Context, tx *gorm.DB, id uuid.UUID) error {
	txx := tx
	if txx == nil {
		txx = r.db
	}
	now := time.Now()
	res := txx.WithContext(ctx).
		Model(&types.OAuthNonce{}).
		Where("id = ? AND used_at IS NULL", id).
		Update("used_at", now)
	if res.Error != nil { return res.Error }
	if res.RowsAffected == 0 { return fmt.Errorf("nonce already used or not found") }
	return nil
}

func (r *oauthNonceRepo) FullDeleteExpires(ctx context.Context, tx *gorm.DB, before time.Time) error {
	txx := tx
	if txx == nil {
		txx = r.db
	}
	return txx.WithContext(ctx).
		Unscoped().
		Where("expires_at < ?", before).
		Delete(&types.OAuthNonce{}).Error
}










