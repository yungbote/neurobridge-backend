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

type UserModelAlertRepo interface {
	Upsert(dbc dbctx.Context, row *types.UserModelAlert) error
	ListByUserAndConceptIDs(dbc dbctx.Context, userID uuid.UUID, conceptIDs []uuid.UUID) ([]*types.UserModelAlert, error)
}

type userModelAlertRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewUserModelAlertRepo(db *gorm.DB, baseLog *logger.Logger) UserModelAlertRepo {
	return &userModelAlertRepo{
		db:  db,
		log: baseLog.With("repo", "UserModelAlertRepo"),
	}
}

func (r *userModelAlertRepo) Upsert(dbc dbctx.Context, row *types.UserModelAlert) error {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}
	if row == nil || row.UserID == uuid.Nil || row.ConceptID == uuid.Nil || row.Kind == "" {
		return nil
	}

	now := time.Now().UTC()
	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	if row.CreatedAt.IsZero() {
		row.CreatedAt = now
	}
	row.UpdatedAt = now
	if row.FirstSeenAt == nil || row.FirstSeenAt.IsZero() {
		t := now
		row.FirstSeenAt = &t
	}
	if row.LastSeenAt == nil || row.LastSeenAt.IsZero() {
		t := now
		row.LastSeenAt = &t
	}
	if row.Occurrences <= 0 {
		row.Occurrences = 1
	}

	return transaction.WithContext(dbc.Ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "user_id"}, {Name: "concept_id"}, {Name: "kind"}},
			DoUpdates: clause.Assignments(map[string]any{
				"severity":      gorm.Expr("EXCLUDED.severity"),
				"score":         gorm.Expr("EXCLUDED.score"),
				"details":       gorm.Expr("EXCLUDED.details"),
				"first_seen_at": gorm.Expr("COALESCE(user_model_alert.first_seen_at, EXCLUDED.first_seen_at)"),
				"last_seen_at":  gorm.Expr("EXCLUDED.last_seen_at"),
				"occurrences":   gorm.Expr("user_model_alert.occurrences + EXCLUDED.occurrences"),
				"resolved_at":   gorm.Expr("EXCLUDED.resolved_at"),
				"updated_at":    gorm.Expr("EXCLUDED.updated_at"),
			}),
		}).
		Create(row).Error
}

func (r *userModelAlertRepo) ListByUserAndConceptIDs(dbc dbctx.Context, userID uuid.UUID, conceptIDs []uuid.UUID) ([]*types.UserModelAlert, error) {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}
	out := []*types.UserModelAlert{}
	if userID == uuid.Nil || len(conceptIDs) == 0 {
		return out, nil
	}
	if err := transaction.WithContext(dbc.Ctx).
		Where("user_id = ? AND concept_id IN ?", userID, conceptIDs).
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}
