package learning

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type TopicStylePreferenceRepo interface {
	Create(ctx context.Context, tx *gorm.DB, rows []*types.TopicStylePreference) ([]*types.TopicStylePreference, error)

	GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.TopicStylePreference, error)
	GetByID(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*types.TopicStylePreference, error)
	Get(ctx context.Context, tx *gorm.DB, userID uuid.UUID, topic string, style string) (*types.TopicStylePreference, error)
	GetByUserIDs(ctx context.Context, tx *gorm.DB, userIDs []uuid.UUID) ([]*types.TopicStylePreference, error)
	GetByUserIDAndTopics(ctx context.Context, tx *gorm.DB, userID uuid.UUID, topics []string) ([]*types.TopicStylePreference, error)

	UpsertEMA(ctx context.Context, tx *gorm.DB, userID uuid.UUID, topic string, style string, reward float64) error
	Upsert(ctx context.Context, tx *gorm.DB, row *types.TopicStylePreference) error
	UpdateFields(ctx context.Context, tx *gorm.DB, id uuid.UUID, updates map[string]interface{}) error

	SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
	SoftDeleteByUserIDs(ctx context.Context, tx *gorm.DB, userIDs []uuid.UUID) error
	FullDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
	FullDeleteByUserIDs(ctx context.Context, tx *gorm.DB, userIDs []uuid.UUID) error
}

type topicStylePreferenceRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewTopicStylePreferenceRepo(db *gorm.DB, baseLog *logger.Logger) TopicStylePreferenceRepo {
	return &topicStylePreferenceRepo{
		db:  db,
		log: baseLog.With("repo", "TopicStylePreferenceRepo"),
	}
}

func (r *topicStylePreferenceRepo) Create(ctx context.Context, tx *gorm.DB, rows []*types.TopicStylePreference) ([]*types.TopicStylePreference, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return []*types.TopicStylePreference{}, nil
	}
	now := time.Now().UTC()
	for _, row := range rows {
		if row == nil {
			continue
		}
		if row.ID == uuid.Nil {
			row.ID = uuid.New()
		}
		if row.UpdatedAt.IsZero() {
			row.UpdatedAt = now
		}
	}
	if err := t.WithContext(ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *topicStylePreferenceRepo) GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.TopicStylePreference, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.TopicStylePreference
	if len(ids) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).Where("id IN ?", ids).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *topicStylePreferenceRepo) GetByID(ctx context.Context, tx *gorm.DB, id uuid.UUID) (*types.TopicStylePreference, error) {
	if id == uuid.Nil {
		return nil, nil
	}
	rows, err := r.GetByIDs(ctx, tx, []uuid.UUID{id})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

func (r *topicStylePreferenceRepo) Get(ctx context.Context, tx *gorm.DB, userID uuid.UUID, topic string, style string) (*types.TopicStylePreference, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	if userID == uuid.Nil {
		return nil, nil
	}
	topic = stringsTrim(topic)
	style = stringsTrim(style)
	if topic == "" || style == "" {
		return nil, nil
	}
	var row types.TopicStylePreference
	err := t.WithContext(ctx).
		Where("user_id = ? AND topic = ? AND style = ?", userID, topic, style).
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

func (r *topicStylePreferenceRepo) GetByUserIDs(ctx context.Context, tx *gorm.DB, userIDs []uuid.UUID) ([]*types.TopicStylePreference, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.TopicStylePreference
	if len(userIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("user_id IN ?", userIDs).
		Order("user_id ASC, updated_at DESC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *topicStylePreferenceRepo) GetByUserIDAndTopics(ctx context.Context, tx *gorm.DB, userID uuid.UUID, topics []string) ([]*types.TopicStylePreference, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	var out []*types.TopicStylePreference
	if userID == uuid.Nil || len(topics) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("user_id = ? AND topic IN ?", userID, topics).
		Order("topic ASC, style ASC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *topicStylePreferenceRepo) UpsertEMA(ctx context.Context, tx *gorm.DB, userID uuid.UUID, topic string, style string, reward float64) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if userID == uuid.Nil {
		return nil
	}
	topic = stringsTrim(topic)
	style = stringsTrim(style)
	if topic == "" || style == "" {
		return nil
	}

	reward = clamp(reward, -1, 1)
	now := time.Now().UTC()

	var row types.TopicStylePreference
	err := t.WithContext(ctx).
		Where("user_id = ? AND topic = ? AND style = ?", userID, topic, style).
		First(&row).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return err
	}

	if row.ID == uuid.Nil {
		row.ID = uuid.New()
		row.UserID = userID
		row.Topic = topic
		row.Style = style
		row.Score = 0
		row.N = 0
	}

	n := row.N + 1
	alpha := 2.0 / float64(n+1)
	if alpha > 0.25 {
		alpha = 0.25
	}
	row.Score = row.Score + alpha*(reward-row.Score)
	row.N = n
	row.UpdatedAt = now

	return t.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "user_id"},
				{Name: "topic"},
				{Name: "style"},
			},
			DoUpdates: clause.AssignmentColumns([]string{"score", "n", "updated_at"}),
		}).
		Create(&row).Error
}

func (r *topicStylePreferenceRepo) Upsert(ctx context.Context, tx *gorm.DB, row *types.TopicStylePreference) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if row == nil || row.UserID == uuid.Nil {
		return nil
	}
	row.Topic = stringsTrim(row.Topic)
	row.Style = stringsTrim(row.Style)
	if row.Topic == "" || row.Style == "" {
		return nil
	}
	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	row.UpdatedAt = time.Now().UTC()

	return t.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "user_id"},
				{Name: "topic"},
				{Name: "style"},
			},
			DoUpdates: clause.AssignmentColumns([]string{"score", "n", "updated_at"}),
		}).
		Create(row).Error
}

func (r *topicStylePreferenceRepo) UpdateFields(ctx context.Context, tx *gorm.DB, id uuid.UUID, updates map[string]interface{}) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if id == uuid.Nil {
		return nil
	}
	if updates == nil {
		updates = map[string]interface{}{}
	}
	if _, ok := updates["updated_at"]; !ok {
		updates["updated_at"] = time.Now().UTC()
	}
	return t.WithContext(ctx).
		Model(&types.TopicStylePreference{}).
		Where("id = ?", id).
		Updates(updates).Error
}

func (r *topicStylePreferenceRepo) SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(ctx).Where("id IN ?", ids).Delete(&types.TopicStylePreference{}).Error
}

func (r *topicStylePreferenceRepo) SoftDeleteByUserIDs(ctx context.Context, tx *gorm.DB, userIDs []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(userIDs) == 0 {
		return nil
	}
	return t.WithContext(ctx).Where("user_id IN ?", userIDs).Delete(&types.TopicStylePreference{}).Error
}

func (r *topicStylePreferenceRepo) FullDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(ctx).Unscoped().Where("id IN ?", ids).Delete(&types.TopicStylePreference{}).Error
}

func (r *topicStylePreferenceRepo) FullDeleteByUserIDs(ctx context.Context, tx *gorm.DB, userIDs []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(userIDs) == 0 {
		return nil
	}
	return t.WithContext(ctx).Unscoped().Where("user_id IN ?", userIDs).Delete(&types.TopicStylePreference{}).Error
}
