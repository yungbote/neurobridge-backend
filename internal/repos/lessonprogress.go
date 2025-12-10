package repos

import (
  "context"
  "github.com/google/uuid"
  "gorm.io/gorm"
  "github.com/yungbote/neurobridge-backend/internal/logger"
  "github.com/yungbote/neurobridge-backend/internal/types"
)

type LessonProgressRepo interface {
  Create(ctx context.Context, tx *gorm.DB, rows []*types.LessonProgress) ([]*types.LessonProgress, error)
  GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.LessonProgress, error)
  GetByUserID(ctx context.Context, tx *gorm.DB, userID uuid.UUID) ([]*types.LessonProgress, error)
  GetByUserAndLessonIDs(ctx context.Context, tx *gorm.DB, userID uuid.UUID, lessonIDs []uuid.UUID) ([]*types.LessonProgress, error)
  Upsert(ctx context.Context, tx *gorm.DB, row *types.LessonProgress) error
  SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
  FullDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error
}

type lessonProgressRepo struct {
  db  *gorm.DB
  log *logger.Logger
}

func NewLessonProgressRepo(db *gorm.DB, baseLog *logger.Logger) LessonProgressRepo {
  repoLog := baseLog.With("repo", "LessonProgressRepo")
  return &lessonProgressRepo{db: db, log: repoLog}
}

func (r *lessonProgressRepo) Create(ctx context.Context, tx *gorm.DB, rows []*types.LessonProgress) ([]*types.LessonProgress, error) {
  transaction := tx
  if transaction == nil {
    transaction = r.db
  }

  if len(rows) == 0 {
    return []*types.LessonProgress{}, nil
  }

  if err := transaction.WithContext(ctx).Create(&rows).Error; err != nil {
    return nil, err
  }
  return rows, nil
}

func (r *lessonProgressRepo) GetByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) ([]*types.LessonProgress, error) {
  transaction := tx
  if transaction == nil {
    transaction = r.db
  }

  var results []*types.LessonProgress
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

func (r *lessonProgressRepo) GetByUserID(ctx context.Context, tx *gorm.DB, userID uuid.UUID) ([]*types.LessonProgress, error) {
  transaction := tx
  if transaction == nil {
    transaction = r.db
  }

  var results []*types.LessonProgress
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

func (r *lessonProgressRepo) GetByUserAndLessonIDs(ctx context.Context, tx *gorm.DB, userID uuid.UUID, lessonIDs []uuid.UUID) ([]*types.LessonProgress, error) {
  transaction := tx
  if transaction == nil {
    transaction = r.db
  }

  var results []*types.LessonProgress
  if userID == uuid.Nil || len(lessonIDs) == 0 {
    return results, nil
  }

  if err := transaction.WithContext(ctx).
    Where("user_id = ? AND lesson_id IN ?", userID, lessonIDs).
    Find(&results).Error; err != nil {
    return nil, err
  }
  return results, nil
}

func (r *lessonProgressRepo) Upsert(ctx context.Context, tx *gorm.DB, row *types.LessonProgress) error {
  transaction := tx
  if transaction == nil {
    transaction = r.db
  }

  if row == nil {
    return nil
  }

  // Upsert by unique user_id + lesson_id
  if err := transaction.WithContext(ctx).
    Where("user_id = ? AND lesson_id = ?", row.UserID, row.LessonID).
    Assign(row).
    FirstOrCreate(row).Error; err != nil {
    return err
  }
  return nil
}

func (r *lessonProgressRepo) SoftDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error {
  transaction := tx
  if transaction == nil {
    transaction = r.db
  }

  if len(ids) == 0 {
    return nil
  }

  if err := transaction.WithContext(ctx).
    Where("id IN ?", ids).
    Delete(&types.LessonProgress{}).Error; err != nil {
    return err
  }
  return nil
}

func (r *lessonProgressRepo) FullDeleteByIDs(ctx context.Context, tx *gorm.DB, ids []uuid.UUID) error {
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
    Delete(&types.LessonProgress{}).Error; err != nil {
    return err
  }
  return nil
}










