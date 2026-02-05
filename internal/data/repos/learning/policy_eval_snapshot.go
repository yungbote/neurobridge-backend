package learning

import (
	"strings"

	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type PolicyEvalSnapshotRepo interface {
	Create(dbc dbctx.Context, row *types.PolicyEvalSnapshot) error
	GetLatestByKey(dbc dbctx.Context, key string) (*types.PolicyEvalSnapshot, error)
}

type policyEvalSnapshotRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewPolicyEvalSnapshotRepo(db *gorm.DB, baseLog *logger.Logger) PolicyEvalSnapshotRepo {
	return &policyEvalSnapshotRepo{db: db, log: baseLog.With("repo", "PolicyEvalSnapshotRepo")}
}

func (r *policyEvalSnapshotRepo) Create(dbc dbctx.Context, row *types.PolicyEvalSnapshot) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if row == nil || strings.TrimSpace(row.PolicyKey) == "" {
		return nil
	}
	return t.WithContext(dbc.Ctx).Create(row).Error
}

func (r *policyEvalSnapshotRepo) GetLatestByKey(dbc dbctx.Context, key string) (*types.PolicyEvalSnapshot, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, nil
	}
	row := &types.PolicyEvalSnapshot{}
	if err := t.WithContext(dbc.Ctx).
		Where("policy_key = ?", key).
		Order("window_end DESC, created_at DESC").
		Limit(1).
		First(row).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return row, nil
}
