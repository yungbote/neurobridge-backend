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

type UserMisconceptionInstanceRepo interface {
	Upsert(dbc dbctx.Context, row *types.UserMisconceptionInstance) error
	ListActiveByUserAndConceptIDs(dbc dbctx.Context, userID uuid.UUID, conceptIDs []uuid.UUID) ([]*types.UserMisconceptionInstance, error)
}

type userMisconceptionInstanceRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewUserMisconceptionInstanceRepo(db *gorm.DB, baseLog *logger.Logger) UserMisconceptionInstanceRepo {
	return &userMisconceptionInstanceRepo{
		db:  db,
		log: baseLog.With("repo", "UserMisconceptionInstanceRepo"),
	}
}

func (r *userMisconceptionInstanceRepo) Upsert(dbc dbctx.Context, row *types.UserMisconceptionInstance) error {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}
	if row == nil || row.UserID == uuid.Nil || row.CanonicalConceptID == uuid.Nil {
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

	return transaction.WithContext(dbc.Ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "user_id"}, {Name: "canonical_concept_id"}, {Name: "pattern_id"}, {Name: "description"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"status",
				"confidence",
				"first_seen_at",
				"last_seen_at",
				"cleared_at",
				"support",
				"updated_at",
			}),
		}).
		Create(row).Error
}

func (r *userMisconceptionInstanceRepo) ListActiveByUserAndConceptIDs(dbc dbctx.Context, userID uuid.UUID, conceptIDs []uuid.UUID) ([]*types.UserMisconceptionInstance, error) {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}
	out := []*types.UserMisconceptionInstance{}
	if userID == uuid.Nil || len(conceptIDs) == 0 {
		return out, nil
	}
	if err := transaction.WithContext(dbc.Ctx).
		Where("user_id = ? AND canonical_concept_id IN ?", userID, conceptIDs).
		Where("status = ?", "active").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}
