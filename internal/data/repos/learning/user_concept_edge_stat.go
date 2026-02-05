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

type UserConceptEdgeStatRepo interface {
	Upsert(dbc dbctx.Context, row *types.UserConceptEdgeStat) error
	ListByUserAndConceptIDs(dbc dbctx.Context, userID uuid.UUID, conceptIDs []uuid.UUID) ([]*types.UserConceptEdgeStat, error)
}

type userConceptEdgeStatRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewUserConceptEdgeStatRepo(db *gorm.DB, baseLog *logger.Logger) UserConceptEdgeStatRepo {
	return &userConceptEdgeStatRepo{
		db:  db,
		log: baseLog.With("repo", "UserConceptEdgeStatRepo"),
	}
}

func (r *userConceptEdgeStatRepo) Upsert(dbc dbctx.Context, row *types.UserConceptEdgeStat) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	if row == nil || row.UserID == uuid.Nil || row.FromConceptID == uuid.Nil || row.ToConceptID == uuid.Nil || row.EdgeType == "" {
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

	return t.WithContext(dbc.Ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "user_id"},
				{Name: "from_concept_id"},
				{Name: "to_concept_id"},
				{Name: "edge_type"},
			},
			DoUpdates: clause.AssignmentColumns([]string{
				"attempts",
				"false_transfers",
				"validated_at",
				"last_transfer_at",
				"last_validation_requested_at",
				"last_false_at",
				"blocked_until",
				"threshold_boost",
				"updated_at",
			}),
		}).
		Create(row).Error
}

func (r *userConceptEdgeStatRepo) ListByUserAndConceptIDs(dbc dbctx.Context, userID uuid.UUID, conceptIDs []uuid.UUID) ([]*types.UserConceptEdgeStat, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	out := []*types.UserConceptEdgeStat{}
	if userID == uuid.Nil || len(conceptIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("user_id = ? AND (from_concept_id IN ? OR to_concept_id IN ?)", userID, conceptIDs, conceptIDs).
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}
