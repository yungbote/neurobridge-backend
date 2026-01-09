package learning

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type TopicMasteryRepo interface {
	Upsert(dbc dbctx.Context, row *types.TopicMastery) error
	ListByUser(dbc dbctx.Context, userID uuid.UUID) ([]*types.TopicMastery, error)
}

type topicMasteryRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewTopicMasteryRepo(db *gorm.DB, baseLog *logger.Logger) TopicMasteryRepo {
	return &topicMasteryRepo{db: db, log: baseLog.With("repo", "TopicMasteryRepo")}
}

func (r *topicMasteryRepo) dbx(dbc dbctx.Context) *gorm.DB {
	if dbc.Tx != nil {
		return dbc.Tx
	}
	return r.db
}

func (r *topicMasteryRepo) ListByUser(dbc dbctx.Context, userID uuid.UUID) ([]*types.TopicMastery, error) {
	out := []*types.TopicMastery{}
	if userID == uuid.Nil {
		return out, nil
	}
	if err := r.dbx(dbc).WithContext(dbc.Ctx).
		Where("user_id = ?", userID).
		Order("updated_at DESC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *topicMasteryRepo) Upsert(dbc dbctx.Context, row *types.TopicMastery) error {
	if row == nil || row.UserID == uuid.Nil || row.Topic == "" {
		return nil
	}
	now := time.Now().UTC()
	row.UpdatedAt = now
	if row.CreatedAt.IsZero() {
		row.CreatedAt = now
	}

	return r.dbx(dbc).WithContext(dbc.Ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "user_id"},
				{Name: "topic"},
			},
			DoUpdates: clause.AssignmentColumns([]string{
				"mastery", "confidence", "last_observed_at", "updated_at",
			}),
		}).
		Create(row).Error
}
