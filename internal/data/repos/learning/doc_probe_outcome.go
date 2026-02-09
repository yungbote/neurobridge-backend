package learning

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type DocProbeOutcomeRepo interface {
	Create(dbc dbctx.Context, row *types.DocProbeOutcome) error
}

type docProbeOutcomeRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewDocProbeOutcomeRepo(db *gorm.DB, baseLog *logger.Logger) DocProbeOutcomeRepo {
	return &docProbeOutcomeRepo{db: db, log: baseLog.With("repo", "DocProbeOutcomeRepo")}
}

func (r *docProbeOutcomeRepo) Create(dbc dbctx.Context, row *types.DocProbeOutcome) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if row == nil || row.UserID == uuid.Nil || row.PathID == uuid.Nil || row.PathNodeID == uuid.Nil || row.BlockID == "" || row.ProbeID == uuid.Nil {
		return nil
	}
	now := time.Now().UTC()
	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	if row.CreatedAt.IsZero() {
		row.CreatedAt = now
	}
	return t.WithContext(dbc.Ctx).Create(row).Error
}
