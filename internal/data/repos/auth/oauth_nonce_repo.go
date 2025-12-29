package auth

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type OAuthNonceRepo interface {
	Create(dbc dbctx.Context, nonces []*types.OAuthNonce) ([]*types.OAuthNonce, error)
	GetByIDs(dbc dbctx.Context, ids []uuid.UUID) ([]*types.OAuthNonce, error)
	MarkUsed(dbc dbctx.Context, id uuid.UUID) error
	FullDeleteExpires(dbc dbctx.Context, before time.Time) error
}

type oauthNonceRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewOAuthNonceRepo(db *gorm.DB, baseLog *logger.Logger) OAuthNonceRepo {
	repoLog := baseLog.With("repo", "OAuthNonceRepo")
	return &oauthNonceRepo{db: db, log: repoLog}
}

func (r *oauthNonceRepo) Create(dbc dbctx.Context, nonces []*types.OAuthNonce) ([]*types.OAuthNonce, error) {
	txx := dbc.Tx
	if txx == nil {
		txx = r.db
	}
	if len(nonces) == 0 {
		return []*types.OAuthNonce{}, nil
	}
	if err := txx.WithContext(dbc.Ctx).Create(&nonces).Error; err != nil {
		return nil, err
	}
	return nonces, nil
}

func (r *oauthNonceRepo) GetByIDs(dbc dbctx.Context, ids []uuid.UUID) ([]*types.OAuthNonce, error) {
	txx := dbc.Tx
	if txx == nil {
		txx = r.db
	}
	var results []*types.OAuthNonce
	if len(ids) == 0 {
		return results, nil
	}
	if err := txx.WithContext(dbc.Ctx).Where("id IN ?", ids).Find(&results).Error; err != nil {
		return nil, err
	}
	return results, nil
}

func (r *oauthNonceRepo) MarkUsed(dbc dbctx.Context, id uuid.UUID) error {
	txx := dbc.Tx
	if txx == nil {
		txx = r.db
	}
	now := time.Now()
	res := txx.WithContext(dbc.Ctx).
		Model(&types.OAuthNonce{}).
		Where("id = ? AND used_at IS NULL", id).
		Update("used_at", now)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("nonce already used or not found")
	}
	return nil
}

func (r *oauthNonceRepo) FullDeleteExpires(dbc dbctx.Context, before time.Time) error {
	txx := dbc.Tx
	if txx == nil {
		txx = r.db
	}
	return txx.WithContext(dbc.Ctx).
		Unscoped().
		Where("expires_at < ?", before).
		Delete(&types.OAuthNonce{}).Error
}
