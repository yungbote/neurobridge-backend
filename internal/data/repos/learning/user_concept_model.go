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

type UserConceptModelRepo interface {
	Upsert(dbc dbctx.Context, row *types.UserConceptModel) error
	Get(dbc dbctx.Context, userID uuid.UUID, conceptID uuid.UUID) (*types.UserConceptModel, error)
	ListByUserAndConceptIDs(dbc dbctx.Context, userID uuid.UUID, conceptIDs []uuid.UUID) ([]*types.UserConceptModel, error)
}

type userConceptModelRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewUserConceptModelRepo(db *gorm.DB, baseLog *logger.Logger) UserConceptModelRepo {
	return &userConceptModelRepo{
		db:  db,
		log: baseLog.With("repo", "UserConceptModelRepo"),
	}
}

func (r *userConceptModelRepo) Get(dbc dbctx.Context, userID uuid.UUID, conceptID uuid.UUID) (*types.UserConceptModel, error) {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}
	if userID == uuid.Nil || conceptID == uuid.Nil {
		return nil, nil
	}
	var row types.UserConceptModel
	err := transaction.WithContext(dbc.Ctx).
		Where("user_id = ? AND canonical_concept_id = ?", userID, conceptID).
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

func (r *userConceptModelRepo) Upsert(dbc dbctx.Context, row *types.UserConceptModel) error {
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
			Columns: []clause.Column{{Name: "user_id"}, {Name: "canonical_concept_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"model_version",
				"active_frames",
				"uncertainty",
				"assumptions",
				"support",
				"last_structural_evidence_at",
				"updated_at",
			}),
		}).
		Create(row).Error
}

func (r *userConceptModelRepo) ListByUserAndConceptIDs(dbc dbctx.Context, userID uuid.UUID, conceptIDs []uuid.UUID) ([]*types.UserConceptModel, error) {
	transaction := dbc.Tx
	if transaction == nil {
		transaction = r.db
	}
	out := []*types.UserConceptModel{}
	if userID == uuid.Nil || len(conceptIDs) == 0 {
		return out, nil
	}
	if err := transaction.WithContext(dbc.Ctx).
		Where("user_id = ? AND canonical_concept_id IN ?", userID, conceptIDs).
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}
