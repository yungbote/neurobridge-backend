package learning

import (
	"context"

	"github.com/google/uuid"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"gorm.io/gorm"
)

type QuizAttemptRepo interface {
	Create(ctx context.Context, tx *gorm.DB, rows []*types.QuizAttempt) ([]*types.QuizAttempt, error)
	GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.QuizAttempt, error)
	ListByLessonIDs(ctx context.Context, tx *gorm.DB, lessonIDs []uuid.UUID) ([]*types.QuizAttempt, error)
	ListByUserIDs(ctx context.Context, tx *gorm.DB, userIDs []uuid.UUID) ([]*types.QuizAttempt, error)
	SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
	FullDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
}

type quizAttemptRepo struct {
	db  *gorm.DB
	log *logger.Logger
}

func NewQuizAttemptRepo(db *gorm.DB, baseLog *logger.Logger) QuizAttemptRepo {
	return &quizAttemptRepo{db: db, log: baseLog.With("repo", "QuizAttemptRepo")}
}

func (r *quizAttemptRepo) Create(ctx context.Context, tx *gorm.DB, rows []*types.QuizAttempt) ([]*types.QuizAttempt, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(rows) == 0 {
		return []*types.QuizAttempt{}, nil
	}
	if err := t.WithContext(ctx).Create(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *quizAttemptRepo) GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.QuizAttempt, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	out := []*types.QuizAttempt{}
	if len(ids) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("id IN ?", ids).
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *quizAttemptRepo) ListByLessonIDs(ctx context.Context, tx *gorm.DB, lessonIDs []uuid.UUID) ([]*types.QuizAttempt, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	out := []*types.QuizAttempt{}
	if len(lessonIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("lesson_id IN ?", lessonIDs).
		Order("created_at DESC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *quizAttemptRepo) ListByUserIDs(ctx context.Context, tx *gorm.DB, userIDs []uuid.UUID) ([]*types.QuizAttempt, error) {
	t := tx
	if t == nil {
		t = r.db
	}
	out := []*types.QuizAttempt{}
	if len(userIDs) == 0 {
		return out, nil
	}
	if err := t.WithContext(ctx).
		Where("user_id IN ?", userIDs).
		Order("created_at DESC").
		Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *quizAttemptRepo) SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(ctx).
		Where("id IN ?", ids).
		Delete(&types.QuizAttempt{}).Error
}

func (r *quizAttemptRepo) FullDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error {
	t := tx
	if t == nil {
		t = r.db
	}
	if len(ids) == 0 {
		return nil
	}
	return t.WithContext(ctx).
		Unscoped().
		Where("id IN ?", ids).
		Delete(&types.QuizAttempt{}).Error
}
