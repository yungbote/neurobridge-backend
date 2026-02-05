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

type UserConceptCalibrationRepo interface {
	Upsert(dbc dbctx.Context, row *types.UserConceptCalibration) error
	ListByUserAndConceptIDs(dbc dbctx.Context, userID uuid.UUID, conceptIDs []uuid.UUID) ([]*types.UserConceptCalibration, error)
}

type userConceptCalibrationRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewUserConceptCalibrationRepo(db *gorm.DB, baseLog *logger.Logger) UserConceptCalibrationRepo {
	return &userConceptCalibrationRepo{
		db:  db,
		log: baseLog.With("repo", "UserConceptCalibrationRepo"),
	}
}

func (r *userConceptCalibrationRepo) Upsert(dbc dbctx.Context, row *types.UserConceptCalibration) error {
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

	return transaction.WithContext(dbc.Ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "user_id"}, {Name: "concept_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"count",
				"expected_sum",
				"observed_sum",
				"brier_sum",
				"abs_err_sum",
				"last_event_at",
				"updated_at",
			}),
		}).
		Create(row).Error
}

func (r *userConceptCalibrationRepo) ListByUserAndConceptIDs(dbc dbctx.Context, userID uuid.UUID, conceptIDs []uuid.UUID) ([]*types.UserConceptCalibration, error) {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}
	out := []*types.UserConceptCalibration{}
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
