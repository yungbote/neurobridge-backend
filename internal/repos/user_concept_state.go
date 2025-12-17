package repos

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/yungbote/neurobridge-backend/internal/logger"
	"github.com/yungbote/neurobridge-backend/internal/types"
)

type UserConceptStateRepo interface {
	UpsertDelta(ctx context.Context, tx *gorm.DB, userID uuid.UUID, conceptID uuid.UUID, newMastery float64, newConfidence float64, lastSeen *time.Time) error
	Get(ctx context.Context, tx *gorm.DB, userID uuid.UUID, conceptID uuid.UUID) (*types.UserConceptState, error)
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

func (r *userConceptStateRepo) Get(ctx context.Context, tx *gorm.DB, userID uuid.UUID, conceptID uuid.UUID) (*types.UserConceptState, error) {
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}
	if userID == uuid.Nil || conceptID == uuid.Nil {
		return nil, nil
	}
	var row types.UserConceptState
	err := transaction.WithContext(ctx).
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

func (r *userConceptStateRepo) UpsertDelta(ctx context.Context, tx *gorm.DB, userID uuid.UUID, conceptID uuid.UUID, newMastery float64, newConfidence float64, lastSeen *time.Time) error {
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}
	if userID == uuid.Nil || conceptID == uuid.Nil {
		return nil
	}

	now := time.Now().UTC()
	row := &types.UserConceptState{
		ID:         uuid.New(),
		UserID:     userID,
		ConceptID:  conceptID,
		Mastery:    newMastery,
		Confidence: newConfidence,
		LastSeenAt: lastSeen,
		UpdatedAt:  now,
	}
	// On conflict, overwrite mastery/confidence/last_seen/updated_at
	return transaction.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "user_id"}, {Name: "concept_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"mastery", "confidence", "last_seen_at", "updated_at",
			}),
		}).
		Create(row).Error
}










