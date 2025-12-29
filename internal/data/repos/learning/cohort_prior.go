package learning

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
)

type CohortPriorRepo interface {
	Create(dbc dbctx.Context, rows []*types.CohortPrior) ([]*types.CohortPrior, error)

	// Read paths
	GetByIDs(dbc dbctx.Context, ids []uuid.UUID) ([]*types.CohortPrior, error)
	GetByConceptIDs(dbc dbctx.Context, conceptIDs []uuid.UUID) ([]*types.CohortPrior, error)
	GetByClusterIDs(dbc dbctx.Context, clusterIDs []uuid.UUID) ([]*types.CohortPrior, error)

	// Safe upsert that works even with NULL concept_id / concept_cluster_id
	Upsert(dbc dbctx.Context, row *types.CohortPrior) error

	SoftDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error
	FullDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error
}

type cohortPriorRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewCohortPriorRepo(db *gorm.DB, baseLog *logger.Logger) CohortPriorRepo {
	return &cohortPriorRepo{db: db, log: baseLog.With("repo", "CohortPriorRepo")}
}

func (r *cohortPriorRepo) Create(dbc dbctx.Context, rows []*types.CohortPrior) ([]*types.CohortPrior, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return []*types.CohortPrior{}, nil
	}
	if err := t.WithContext(dbc.Ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *cohortPriorRepo) GetByIDs(dbc dbctx.Context, ids []uuid.UUID) ([]*types.CohortPrior, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.CohortPrior
	if len(ids) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).Where("id IN ?", ids).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *cohortPriorRepo) GetByConceptIDs(dbc dbctx.Context, conceptIDs []uuid.UUID) ([]*types.CohortPrior, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.CohortPrior
	if len(conceptIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("concept_id IN ?", conceptIDs).
		Order("concept_id ASC, updated_at DESC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *cohortPriorRepo) GetByClusterIDs(dbc dbctx.Context, clusterIDs []uuid.UUID) ([]*types.CohortPrior, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.CohortPrior
	if len(clusterIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("concept_cluster_id IN ?", clusterIDs).
		Order("concept_cluster_id ASC, updated_at DESC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// Upsert that is NULL-safe by doing a lookup using IS NOT DISTINCT FROM, then update-or-insert.
func (r *cohortPriorRepo) Upsert(dbc dbctx.Context, row *types.CohortPrior) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if row == nil {
		return nil
	}
	row.UpdatedAt = time.Now().UTC()

	var existing types.CohortPrior
	err := t.WithContext(dbc.Ctx).
		Where(
			"concept_id IS NOT DISTINCT FROM ? AND concept_cluster_id IS NOT DISTINCT FROM ? AND activity_kind = ? AND modality = ? AND variant = ?",
			row.ConceptID, row.ConceptClusterID, row.ActivityKind, row.Modality, row.Variant,
		).
		Limit(1).
		Find(&existing).Error
	if err != nil {
		return err
	}

	if existing.ID != uuid.Nil {
		return t.WithContext(dbc.Ctx).
			Model(&types.CohortPrior{}).
			Where("id = ?", existing.ID).
			Updates(map[string]interface{}{
				"ema":              row.EMA,
				"n":                row.N,
				"a":                row.A,
				"b":                row.B,
				"completion_rate":  row.CompletionRate,
				"median_dwell_ms":  row.MedianDwellMS,
				"last_observed_at": row.LastObservedAt,
				"updated_at":       row.UpdatedAt,
			}).Error
	}

	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	if row.N == 0 && row.A == 0 && row.B == 0 {
		// defensive defaults (matches your model defaults but safe for programmatic inserts)
		row.A = 1
		row.B = 1
	}
	return t.WithContext(dbc.Ctx).Create(row).Error
}

func (r *cohortPriorRepo) SoftDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Where("id IN ?", ids).Delete(&types.CohortPrior{}).Error
}

func (r *cohortPriorRepo) FullDeleteByIDs(dbc dbctx.Context, ids []uuid.UUID) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(dbc.Ctx).Unscoped().Where("id IN ?", ids).Delete(&types.CohortPrior{}).Error
}
