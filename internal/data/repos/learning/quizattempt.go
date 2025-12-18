package learning

import (
	"context"
	"github.com/google/uuid"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"gorm.io/gorm"
)

type QuizAttemptRepo interface {
	Create(ctx context.Context, tx *gorm.DB, attempts []*types.QuizAttempt) ([]*types.QuizAttempt, error)
	GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.QuizAttempt, error)
	GetByUserID(ctx context.Context, tx *gorm.DB, userID uuid.UUID) ([]*types.QuizAttempt, error)
	GetByLessonIDs(ctx context.Context, tx *gorm.DB, lessonIDs []uuid.UUID) ([]*types.QuizAttempt, error)
	SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
	FullDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
}

type quizAttemptRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewQuizAttemptRepo(db *gorm.DB, baseLog *logger.Logger) QuizAttemptRepo {
	repoLog := baseLog.With("repo", "QuizAttemptRepo")
	return &quizAttemptRepo{db: db, log: repoLog}
}

func (r *quizAttemptRepo) Create(ctx context.Context, tx *gorm.DB, attempts []*types.QuizAttempt) ([]*types.QuizAttempt, error) {
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}

	if len(attempts) == 0 {
		return []*types.QuizAttempt{}, nil
	}

	if err := transaction.WithContext(ctx).Create(&attempts).Error; err != nil {
		return nil, err
	}
	return attempts, nil
}

func (r *quizAttemptRepo) GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.QuizAttempt, error) {
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}

	var results []*types.QuizAttempt
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

func (r *quizAttemptRepo) GetByUserID(ctx context.Context, tx *gorm.DB, userID uuid.UUID) ([]*types.QuizAttempt, error) {
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}

	var results []*types.QuizAttempt
	if userID == uuid.Nil {
		return results, nil
	}

	if err := transaction.WithContext(ctx).
		Where("user_id = ?", userID).
		Find(&results).Error; err != nil {
		return nil, err
	}
	return results, nil
}

func (r *quizAttemptRepo) GetByLessonIDs(ctx context.Context, tx *gorm.DB, lessonIDs []uuid.UUID) ([]*types.QuizAttempt, error) {
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}

	var results []*types.QuizAttempt
	if len(lessonIDs) == 0 {
		return results, nil
	}

	if err := transaction.WithContext(ctx).
		Where("lesson_id IN ?", lessonIDs).
		Find(&results).Error; err != nil {
		return nil, err
	}
	return results, nil
}

func (r *quizAttemptRepo) SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error {
	transaction := tx
	if transaction == nil {
		transaction = r.db
	}

	if len(ids) == 0 {
		return nil
	}

	if err := transaction.WithContext(ctx).
		Where("id IN ?", ids).
		Delete(&types.QuizAttempt{}).Error; err != nil {
		return err
	}
	return nil
}

func (r *quizAttemptRepo) FullDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error {
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
		Delete(&types.QuizAttempt{}).Error; err != nil {
		return err
	}
	return nil
}
