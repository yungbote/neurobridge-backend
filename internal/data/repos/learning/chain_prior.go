package learning

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type ChainPriorRepo interface {
	Upsert(ctx context.Context, tx *gorm.DB, row *types.ChainPrior) error
	GetByChainKeys(ctx context.Context, tx *gorm.DB, chainKeys []string) ([]*types.ChainPrior, error)
}

type chainPriorRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewChainPriorRepo(db *gorm.DB, baseLog *logger.Logger) ChainPriorRepo {
	return &chainPriorRepo{db: db, log: baseLog.With("repo", "ChainPriorRepo")}
}

func (r *chainPriorRepo) GetByChainKeys(ctx context.Context, tx *gorm.DB, chainKeys []string) ([]*types.ChainPrior, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.ChainPrior
	if len(chainKeys) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("chain_key IN ?", chainKeys).
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *chainPriorRepo) Upsert(ctx context.Context, tx *gorm.DB, row *types.ChainPrior) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if row == nil || row.ChainKey == "" || row.ActivityKind == "" || row.Modality == "" || row.Variant == "" || row.RepresentationKey == "" {
		return nil
	}
	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	now := time.Now().UTC()
	row.UpdatedAt = now

	return t.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "chain_key"},
				{Name: "cohort_key"},
				{Name: "activity_kind"},
				{Name: "modality"},
				{Name: "variant"},
				{Name: "representation_key"},
			},
			DoUpdates: clause.AssignmentColumns([]string{
				"ema", "n", "a", "b", "completion_rate", "median_dwell_ms", "last_observed_at", "updated_at",
			}),
		}).
		Create(row).Error
}
