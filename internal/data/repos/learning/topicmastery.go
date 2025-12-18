package learning

import (
	"context"
	"github.com/google/uuid"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"gorm.io/gorm"
)

type TopicMasteryRepo interface {
	Create(ctx context.Context, tx *gorm.DB, rows []*types.TopicMastery) ([]*types.TopicMastery, error)
	GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.TopicMastery, error)
	GetByUserIDs(ctx context.Context, tx *gorm.DB, userIDs []uuid.UUID) ([]*types.TopicMastery, error)
	GetByUserIDAndTopics(ctx context.Context, tx *gorm.DB, userID uuid.UUID, topics []string) ([]*types.TopicMastery, error)
	Update(ctx context.Context, tx *gorm.DB, row *types.TopicMastery) error
	SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
	FullDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
}

type topicMasteryRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewTopicMasteryRepo(db *gorm.DB, baseLog *logger.Logger) TopicMasteryRepo {
	repoLog := baseLog.With("repo", "TopicMasteryRepo")
	return &topicMasteryRepo{db: db, log: repoLog}
}

func (r *topicMasteryRepo) Create(ctx context.Context, tx *gorm.DB, rows []*types.TopicMastery) ([]*types.TopicMastery, error) {
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}

	if len(rows) == 0 {
		return []*types.TopicMastery{}, nil
	}

	if err := transaction.WithContext(ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *topicMasteryRepo) GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.TopicMastery, error) {
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}

	var results []*types.TopicMastery
	if len(ids) == 0 {
		return results, nil
	}

	if err := transaction.WithContext(ctx).
		Where("id IN ?", ids).
		Find(&results).Error; err != nil {
		return nil, err
	}
	return results, nil
}

func (r *topicMasteryRepo) GetByUserIDs(ctx context.Context, tx *gorm.DB, userIDs []uuid.UUID) ([]*types.TopicMastery, error) {
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}

	var results []*types.TopicMastery
	if len(userIDs) == 0 {
		return results, nil
	}

	if err := transaction.WithContext(ctx).
		Where("user_id IN ?", userIDs).
		Find(&results).Error; err != nil {
		return nil, err
	}
	return results, nil
}

func (r *topicMasteryRepo) GetByUserIDAndTopics(ctx context.Context, tx *gorm.DB, userID uuid.UUID, topics []string) ([]*types.TopicMastery, error) {
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}

	var results []*types.TopicMastery
	if len(topics) == 0 {
		return results, nil
	}

	if err := transaction.WithContext(ctx).
		Where("user_id = ? AND topic IN ?", userID, topics).
		Find(&results).Error; err != nil {
		return nil, err
	}
	return results, nil
}

func (r *topicMasteryRepo) Update(ctx context.Context, tx *gorm.DB, row *types.TopicMastery) error {
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}

	if row == nil {
		return nil
	}

	if err := transaction.WithContext(ctx).Save(row).Error; err != nil {
		return err
	}
	return nil
}

func (r *topicMasteryRepo) SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error {
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}

	if len(ids) == 0 {
		return nil
	}

	if err := transaction.WithContext(ctx).
		Where("id IN ?", ids).
		Delete(&types.TopicMastery{}).Error; err != nil {
		return err
	}
	return nil
}

func (r *topicMasteryRepo) FullDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error {
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}

	if len(ids) == 0 {
		return nil
	}

	if err := transaction.WithContext(ctx).
		Unscoped().
		Where("id IN ?", ids).
		Delete(&types.TopicMastery{}).Error; err != nil {
		return err
	}
	return nil
}
