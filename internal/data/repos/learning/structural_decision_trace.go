package learning

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type StructuralDecisionTraceRepo interface {
	Create(dbc dbctx.Context, row *types.StructuralDecisionTrace) error
	CreateMany(dbc dbctx.Context, rows []*types.StructuralDecisionTrace) ([]*types.StructuralDecisionTrace, error)
}

type structuralDecisionTraceRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewStructuralDecisionTraceRepo(db *gorm.DB, baseLog *logger.Logger) StructuralDecisionTraceRepo {
	return &structuralDecisionTraceRepo{db: db, log: baseLog.With("repo", "StructuralDecisionTraceRepo")}
}

func (r *structuralDecisionTraceRepo) Create(dbc dbctx.Context, row *types.StructuralDecisionTrace) error {
	if row == nil {
		return nil
	}
	rows, err := r.CreateMany(dbc, []*types.StructuralDecisionTrace{row})
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	return nil
}

func (r *structuralDecisionTraceRepo) CreateMany(dbc dbctx.Context, rows []*types.StructuralDecisionTrace) ([]*types.StructuralDecisionTrace, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return []*types.StructuralDecisionTrace{}, nil
	}
	now := time.Now().UTC()
	for _, row := range rows {
		if row == nil {
			continue
		}
		if row.ID == uuid.Nil {
			row.ID = uuid.New()
		}
		if row.OccurredAt.IsZero() {
			row.OccurredAt = now
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
