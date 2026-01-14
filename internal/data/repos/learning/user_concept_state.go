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

type UserConceptStateRepo interface {
	Upsert(dbc dbctx.Context, row *types.UserConceptState) error
	Get(dbc dbctx.Context, userID uuid.UUID, conceptID uuid.UUID) (*types.UserConceptState, error)
	ListByUserAndConceptIDs(dbc dbctx.Context, userID uuid.UUID, conceptIDs []uuid.UUID) ([]*types.UserConceptState, error)
}

type userConceptStateRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewUserConceptStateRepo(db *gorm.DB, baseLog *logger.Logger) UserConceptStateRepo {
	return &userConceptStateRepo{
		db:  db,
		log: baseLog.With("repo", "UserConceptStateRepo"),
	}
}

func (r *userConceptStateRepo) Get(dbc dbctx.Context, userID uuid.UUID, conceptID uuid.UUID) (*types.UserConceptState, error) {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}
	if userID == uuid.Nil || conceptID == uuid.Nil {
		return nil, nil
	}
	var row types.UserConceptState
	err := transaction.WithContext(dbc.Ctx).
		Where("user_id = ? AND concept_id = ?", userID, conceptID).
		Limit(1).
		Find(&row).Error
	if err != nil {
		return nil, err
	}
	if row.ID == uuid.Nil {
		return nil, nil
	}
	return &row, nil
}

func (r *userConceptStateRepo) Upsert(dbc dbctx.Context, row *types.UserConceptState) error {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}
	if row == nil || row.UserID == uuid.Nil || row.ConceptID == uuid.Nil {
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

	// On conflict, overwrite state fields (job cursor provides idempotency).
	return transaction.WithContext(dbc.Ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "user_id"}, {Name: "concept_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"mastery",
				"confidence",
				"last_seen_at",
				"next_review_at",
				"decay_rate",
				"misconceptions",
				"attempts",
				"correct",
				"updated_at",
			}),
		}).
		Create(row).Error
}

func (r *userConceptStateRepo) ListByUserAndConceptIDs(dbc dbctx.Context, userID uuid.UUID, conceptIDs []uuid.UUID) ([]*types.UserConceptState, error) {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}
	out := []*types.UserConceptState{}
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
