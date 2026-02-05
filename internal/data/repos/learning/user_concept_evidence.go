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

type UserConceptEvidenceRepo interface {
	CreateIgnoreDuplicates(dbc dbctx.Context, rows []*types.UserConceptEvidence) error
}

type userConceptEvidenceRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewUserConceptEvidenceRepo(db *gorm.DB, baseLog *logger.Logger) UserConceptEvidenceRepo {
	return &userConceptEvidenceRepo{
		db:  db,
		log: baseLog.With("repo", "UserConceptEvidenceRepo"),
	}
}

func (r *userConceptEvidenceRepo) CreateIgnoreDuplicates(dbc dbctx.Context, rows []*types.UserConceptEvidence) error {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}
	if len(rows) == 0 {
		return nil
	}
	now := time.Now().UTC()
	filtered := make([]*types.UserConceptEvidence, 0, len(rows))
	for _, row := range rows {
		if row == nil || row.UserID == uuid.Nil || row.ConceptID == uuid.Nil {
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
		row.UpdatedAt = now
		filtered = append(filtered, row)
	}
	if len(filtered) == 0 {
		return nil
	}
	return transaction.WithContext(dbc.Ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "user_id"}, {Name: "concept_id"}, {Name: "source"}, {Name: "source_ref"}},
			DoNothing: true,
		}).
		Create(&filtered).Error
}
