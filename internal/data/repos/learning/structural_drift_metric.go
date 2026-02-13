package learning

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type StructuralDriftMetricRepo interface {
	Create(dbc dbctx.Context, row *types.StructuralDriftMetric) error
	CreateMany(dbc dbctx.Context, rows []*types.StructuralDriftMetric) ([]*types.StructuralDriftMetric, error)
}

type structuralDriftMetricRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewStructuralDriftMetricRepo(db *gorm.DB, baseLog *logger.Logger) StructuralDriftMetricRepo {
	return &structuralDriftMetricRepo{db: db, log: baseLog.With("repo", "StructuralDriftMetricRepo")}
}

func (r *structuralDriftMetricRepo) Create(dbc dbctx.Context, row *types.StructuralDriftMetric) error {
	if row == nil {
		return nil
	}
	_, err := r.CreateMany(dbc, []*types.StructuralDriftMetric{row})
	return err
}

func (r *structuralDriftMetricRepo) CreateMany(dbc dbctx.Context, rows []*types.StructuralDriftMetric) ([]*types.StructuralDriftMetric, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return []*types.StructuralDriftMetric{}, nil
	}
	now := time.Now().UTC()
	for _, row := range rows {
		if row == nil {
			continue
		}
		if row.ID == uuid.Nil {
			row.ID = uuid.New()
		}
		if row.CreatedAt.IsZero() {
			row.CreatedAt = now
		}
	}
	if err := t.WithContext(dbc.Ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}
