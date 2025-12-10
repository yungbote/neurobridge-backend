package repos

import (
  "context"
  "github.com/google/uuid"
  "gorm.io/gorm"
  "github.com/yungbote/neurobridge-backend/internal/logger"
  "github.com/yungbote/neurobridge-backend/internal/types"
)

type QuizQuestionRepo interface {
  Create(ctx context.Context, tx *gorm.DB, questions []*types.QuizQuestion) ([]*types.QuizQuestion, error)
  GetByIDs(ctx context.Context, tx *gorm.DB, questionIDs []uuid.UUID) ([]*types.QuizQuestion, error)
  GetByLessonIDs(ctx context.Context, tx *gorm.DB, lessonIDs []uuid.UUID) ([]*types.QuizQuestion, error)
  SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, questionIDs []uuid.UUID) error
  SoftDeleteByLessonIDs(ctx context.Context, tx *gorm.DB, lessonIDs []uuid.UUID) error
  FullDeleteByIDs(ctx context.Context, tx *gorm.DB, questionIDs []uuid.UUID) error
  FullDeleteByLessonIDs(ctx context.Context, tx *gorm.DB, lessonIDs []uuid.UUID) error
}

type quizQuestionRepo struct {
  db  *gorm.DB
  log *logger.Logger
}

func NewQuizQuestionRepo(db *gorm.DB, baseLog *logger.Logger) QuizQuestionRepo {
  repoLog := baseLog.With("repo", "QuizQuestionRepo")
  return &quizQuestionRepo{db: db, log: repoLog}
}

func (r *quizQuestionRepo) Create(ctx context.Context, tx *gorm.DB, questions []*types.QuizQuestion) ([]*types.QuizQuestion, error) {
  transaction := tx
  if transaction == nil {
    transaction = r.db
  }

  if len(questions) == 0 {
    return []*types.QuizQuestion{}, nil
  }

  if err := transaction.WithContext(ctx).Create(&questions).Error; err != nil {
    return nil, err
  }
  return questions, nil
}

func (r *quizQuestionRepo) GetByIDs(ctx context.Context, tx *gorm.DB, questionIDs []uuid.UUID) ([]*types.QuizQuestion, error) {
  transaction := tx
  if transaction == nil {
    transaction = r.db
  }

  var results []*types.QuizQuestion
  if len(questionIDs) == 0 {
    return results, nil
  }

  if err := transaction.WithContext(ctx).
    Where("id IN ?", questionIDs).
    Find(&results).Error; err != nil {
    return nil, err
  }
  return results, nil
}

func (r *quizQuestionRepo) GetByLessonIDs(ctx context.Context, tx *gorm.DB, lessonIDs []uuid.UUID) ([]*types.QuizQuestion, error) {
  transaction := tx
  if transaction == nil {
    transaction = r.db
  }

  var results []*types.QuizQuestion
  if len(lessonIDs) == 0 {
    return results, nil
  }

  if err := transaction.WithContext(ctx).
    Where("lesson_id IN ?", lessonIDs).
    Order("lesson_id, index ASC").
    Find(&results).Error; err != nil {
    return nil, err
  }
  return results, nil
}

func (r *quizQuestionRepo) SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, questionIDs []uuid.UUID) error {
  transaction := tx
  if transaction == nil {
    transaction = r.db
  }

  if len(questionIDs) == 0 {
    return nil
  }

  if err := transaction.WithContext(ctx).
    Where("id IN ?", questionIDs).
    Delete(&types.QuizQuestion{}).Error; err != nil {
    return err
  }
  return nil
}

func (r *quizQuestionRepo) SoftDeleteByLessonIDs(ctx context.Context, tx *gorm.DB, lessonIDs []uuid.UUID) error {
  transaction := tx
  if transaction == nil {
    transaction = r.db
  }

  if len(lessonIDs) == 0 {
    return nil
  }

  if err := transaction.WithContext(ctx).
    Where("lesson_id IN ?", lessonIDs).
    Delete(&types.QuizQuestion{}).Error; err != nil {
    return err
  }
  return nil
}

func (r *quizQuestionRepo) FullDeleteByIDs(ctx context.Context, tx *gorm.DB, questionIDs []uuid.UUID) error {
  transaction := tx
  if transaction == nil {
    transaction = r.db
  }

  if len(questionIDs) == 0 {
    return nil
  }

  if err := transaction.WithContext(ctx).
    Unscoped().
    Where("id IN ?", questionIDs).
    Delete(&types.QuizQuestion{}).Error; err != nil {
    return err
  }
  return nil
}

func (r *quizQuestionRepo) FullDeleteByLessonIDs(ctx context.Context, tx *gorm.DB, lessonIDs []uuid.UUID) error {
  transaction := tx
  if transaction == nil {
    transaction = r.db
  }

  if len(lessonIDs) == 0 {
    return nil
  }

  if err := transaction.WithContext(ctx).
    Unscoped().
    Where("lesson_id IN ?", lessonIDs).
    Delete(&types.QuizQuestion{}).Error; err != nil {
    return err
  }
  return nil
}










