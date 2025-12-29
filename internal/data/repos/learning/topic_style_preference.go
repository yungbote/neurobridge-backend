package learning

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
)

type TopicStylePreferenceRepo interface {
	Upsert(dbc dbctx.Context, row *types.TopicStylePreference) error
	ListByUser(dbc dbctx.Context, userID uuid.UUID) ([]*types.TopicStylePreference, error)
}

type topicStylePreferenceRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewTopicStylePreferenceRepo(db *gorm.DB, baseLog *logger.Logger) TopicStylePreferenceRepo {
	return &topicStylePreferenceRepo{db: db, log: baseLog.With("repo", "TopicStylePreferenceRepo")}
}

func (r *topicStylePreferenceRepo) dbx(dbc dbctx.Context) *gorm.DB {
	if dbc.Tx != nil {
		return dbc.Tx
	}
	return r.db
}

func (r *topicStylePreferenceRepo) ListByUser(dbc dbctx.Context, userID uuid.UUID) ([]*types.TopicStylePreference, error) {
	out := []*types.TopicStylePreference{}
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

func (r *topicStylePreferenceRepo) Upsert(dbc dbctx.Context, row *types.TopicStylePreference) error {
	if row == nil || row.UserID == uuid.Nil || row.Topic == "" || row.Modality == "" {
		return nil
	}
	if row.Variant == "" {
		row.Variant = "default"
	}

	now := time.Now().UTC()
	row.UpdatedAt = now

	return r.dbx(dbc).WithContext(dbc.Ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "user_id"},
				{Name: "topic"},
				{Name: "modality"},
				{Name: "variant"},
			},
			DoUpdates: clause.AssignmentColumns([]string{
				"ema", "n", "a", "b", "last_observed_at", "updated_at",
			}),
		}).
		Create(row).Error
}
