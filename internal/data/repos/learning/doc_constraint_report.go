package learning

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type DocConstraintReportRepo interface {
	GetByID(dbc dbctx.Context, id uuid.UUID) (*types.DocConstraintReport, error)
	GetByReportID(dbc dbctx.Context, reportID string) (*types.DocConstraintReport, error)
	GetByVariantID(dbc dbctx.Context, variantID uuid.UUID) (*types.DocConstraintReport, error)
	Upsert(dbc dbctx.Context, row *types.DocConstraintReport) error
}

type docConstraintReportRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewDocConstraintReportRepo(db *gorm.DB, baseLog *logger.Logger) DocConstraintReportRepo {
	return &docConstraintReportRepo{db: db, log: baseLog.With("repo", "DocConstraintReportRepo")}
}

func (r *docConstraintReportRepo) GetByID(dbc dbctx.Context, id uuid.UUID) (*types.DocConstraintReport, error) {
	if id == uuid.Nil {
		return nil, nil
	}
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out types.DocConstraintReport
	if err := t.WithContext(dbc.Ctx).First(&out, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &out, nil
}

func (r *docConstraintReportRepo) GetByReportID(dbc dbctx.Context, reportID string) (*types.DocConstraintReport, error) {
	if reportID == "" {
		return nil, nil
	}
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out types.DocConstraintReport
	if err := t.WithContext(dbc.Ctx).First(&out, "report_id = ?", reportID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &out, nil
}

func (r *docConstraintReportRepo) GetByVariantID(dbc dbctx.Context, variantID uuid.UUID) (*types.DocConstraintReport, error) {
	if variantID == uuid.Nil {
		return nil, nil
	}
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out types.DocConstraintReport
	if err := t.WithContext(dbc.Ctx).First(&out, "variant_id = ?", variantID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &out, nil
}

func (r *docConstraintReportRepo) Upsert(dbc dbctx.Context, row *types.DocConstraintReport) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if row == nil {
		return nil
	}
	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	if row.CreatedAt.IsZero() {
		row.CreatedAt = time.Now().UTC()
	}

	return t.WithContext(dbc.Ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "report_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"variant_id",
				"trace_id",
				"schema_version",
				"passed",
				"violation_count",
				"report_json",
			}),
		}).
		Create(row).Error
}
