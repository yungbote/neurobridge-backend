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

type MisconceptionResolutionStateRepo interface {
	GetByUserAndConceptID(dbc dbctx.Context, userID, conceptID uuid.UUID) (*types.MisconceptionResolutionState, error)
	ListByUserAndConceptIDs(dbc dbctx.Context, userID uuid.UUID, conceptIDs []uuid.UUID) ([]*types.MisconceptionResolutionState, error)
	Upsert(dbc dbctx.Context, row *types.MisconceptionResolutionState) error
}

type misconceptionResolutionStateRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewMisconceptionResolutionStateRepo(db *gorm.DB, baseLog *logger.Logger) MisconceptionResolutionStateRepo {
	return &misconceptionResolutionStateRepo{db: db, log: baseLog.With("repo", "MisconceptionResolutionStateRepo")}
}

func (r *misconceptionResolutionStateRepo) GetByUserAndConceptID(dbc dbctx.Context, userID, conceptID uuid.UUID) (*types.MisconceptionResolutionState, error) {
	if userID == uuid.Nil || conceptID == uuid.Nil {
		return nil, nil
	}
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out types.MisconceptionResolutionState
	if err := t.WithContext(dbc.Ctx).
		Where("user_id = ? AND concept_id = ?", userID, conceptID).
		Limit(1).
		Find(&out).Error; err != nil {
		return nil, err
	}
	if out.ID == uuid.Nil {
		return nil, nil
	}
	return &out, nil
}

func (r *misconceptionResolutionStateRepo) ListByUserAndConceptIDs(dbc dbctx.Context, userID uuid.UUID, conceptIDs []uuid.UUID) ([]*types.MisconceptionResolutionState, error) {
	if userID == uuid.Nil || len(conceptIDs) == 0 {
		return []*types.MisconceptionResolutionState{}, nil
	}
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	var out []*types.MisconceptionResolutionState
	if err := t.WithContext(dbc.Ctx).
		Where("user_id = ? AND concept_id IN ?", userID, conceptIDs).
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *misconceptionResolutionStateRepo) Upsert(dbc dbctx.Context, row *types.MisconceptionResolutionState) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if row == nil || row.UserID == uuid.Nil || row.ConceptID == uuid.Nil {
		return nil
	}
	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	if row.CreatedAt.IsZero() {
		row.CreatedAt = time.Now().UTC()
	}
	row.UpdatedAt = time.Now().UTC()

	return t.WithContext(dbc.Ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "user_id"}, {Name: "concept_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"status",
				"correct_count",
				"incorrect_count",
				"required_correct",
				"last_correct_at",
				"last_incorrect_at",
				"resolved_at",
				"relapsed_at",
				"next_review_at",
				"evidence_json",
				"updated_at",
			}),
		}).
		Create(row).Error
}
