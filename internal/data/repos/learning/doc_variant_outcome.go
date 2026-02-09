package learning

import (
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type DocVariantOutcomeRepo interface {
	Create(dbc dbctx.Context, row *types.DocVariantOutcome) error
}

type docVariantOutcomeRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewDocVariantOutcomeRepo(db *gorm.DB, baseLog *logger.Logger) DocVariantOutcomeRepo {
	return &docVariantOutcomeRepo{db: db, log: baseLog.With("repo", "DocVariantOutcomeRepo")}
}

func (r *docVariantOutcomeRepo) Create(dbc dbctx.Context, row *types.DocVariantOutcome) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if row == nil || row.UserID == uuid.Nil || row.PathID == uuid.Nil || row.PathNodeID == uuid.Nil {
		return nil
	}
	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	if row.CreatedAt.IsZero() {
		row.CreatedAt = time.Now().UTC()
	}
	row.PolicyVersion = strings.TrimSpace(row.PolicyVersion)
	if row.PolicyVersion == "" {
		row.PolicyVersion = "base"
	}
	row.OutcomeKind = strings.TrimSpace(row.OutcomeKind)
	if row.OutcomeKind == "" {
		row.OutcomeKind = "eval_v1"
	}
	// Upsert on exposure_id so late evaluations can overwrite earlier snapshots.
	return t.WithContext(dbc.Ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "exposure_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"metrics_json", "policy_version", "schema_version", "outcome_kind", "created_at"}),
		}).
		Create(row).Error
}
