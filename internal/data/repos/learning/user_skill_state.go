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

type UserSkillStateRepo interface {
	Upsert(dbc dbctx.Context, row *types.UserSkillState) error
	ListByUserAndConceptIDs(dbc dbctx.Context, userID uuid.UUID, conceptIDs []uuid.UUID) ([]*types.UserSkillState, error)
}

type userSkillStateRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewUserSkillStateRepo(db *gorm.DB, baseLog *logger.Logger) UserSkillStateRepo {
	return &userSkillStateRepo{db: db, log: baseLog.With("repo", "UserSkillStateRepo")}
}

func (r *userSkillStateRepo) Upsert(dbc dbctx.Context, row *types.UserSkillState) error {
	t := dbc.Tx
	if t == nil {
		t = r.db
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

	return t.WithContext(dbc.Ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "user_id"}, {Name: "concept_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"theta",
				"sigma",
				"count",
				"last_event_at",
				"updated_at",
			}),
		}).
		Create(row).Error
}

func (r *userSkillStateRepo) ListByUserAndConceptIDs(dbc dbctx.Context, userID uuid.UUID, conceptIDs []uuid.UUID) ([]*types.UserSkillState, error) {
	t := dbc.Tx
	if t == nil {
		t = r.db
	}
	out := []*types.UserSkillState{}
	if userID == uuid.Nil || len(conceptIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(dbc.Ctx).
		Where("user_id = ? AND concept_id IN ?", userID, conceptIDs).
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}
