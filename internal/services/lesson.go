package services

import (
  "context"
  "fmt"

  "github.com/google/uuid"
  "gorm.io/gorm"

  "github.com/yungbote/neurobridge-backend/internal/logger"
  "github.com/yungbote/neurobridge-backend/internal/repos"
  "github.com/yungbote/neurobridge-backend/internal/requestdata"
  "github.com/yungbote/neurobridge-backend/internal/types"
)

type LessonService interface {
  ListLessonsForModule(ctx context.Context, tx *gorm.DB, moduleID uuid.UUID) ([]*types.Lesson, error)
}

type lessonService struct {
  db         *gorm.DB
  log        *logger.Logger
  courseRepo repos.CourseRepo
  moduleRepo repos.CourseModuleRepo
  lessonRepo repos.LessonRepo
}

func NewLessonService(
  db *gorm.DB,
  baseLog *logger.Logger,
  courseRepo repos.CourseRepo,
  moduleRepo repos.CourseModuleRepo,
  lessonRepo repos.LessonRepo,
) LessonService {
  return &lessonService{
    db:         db,
    log:        baseLog.With("service", "LessonService"),
    courseRepo: courseRepo,
    moduleRepo: moduleRepo,
    lessonRepo: lessonRepo,
  }
}

func (s *lessonService) ListLessonsForModule(ctx context.Context, tx *gorm.DB, moduleID uuid.UUID) ([]*types.Lesson, error) {
  rd := requestdata.GetRequestData(ctx)
  if rd == nil || rd.UserID == uuid.Nil {
    return nil, fmt.Errorf("not authenticated")
  }
  if moduleID == uuid.Nil {
    return nil, fmt.Errorf("missing module id")
  }

  transaction := tx
  if transaction == nil {
    transaction = s.db
  }

  // Load module
  mods, err := s.moduleRepo.GetByIDs(ctx, transaction, []uuid.UUID{moduleID})
  if err != nil {
    s.log.Warn("ListLessonsForModule: load module failed", "error", err, "module_id", moduleID)
    return nil, err
  }
  if len(mods) == 0 || mods[0] == nil {
    return nil, fmt.Errorf("module not found")
  }
  mod := mods[0]

  // Ownership check: module's course must belong to user
  courses, err := s.courseRepo.GetByIDs(ctx, transaction, []uuid.UUID{mod.CourseID})
  if err != nil {
    s.log.Warn("ListLessonsForModule: load course failed", "error", err, "course_id", mod.CourseID)
    return nil, err
  }
  if len(courses) == 0 || courses[0] == nil || courses[0].UserID != rd.UserID {
    return nil, fmt.Errorf("module not found")
  }

  lessons, err := s.lessonRepo.GetByModuleIDs(ctx, transaction, []uuid.UUID{moduleID})
  if err != nil {
    s.log.Warn("ListLessonsForModule: load lessons failed", "error", err, "module_id", moduleID)
    return nil, err
  }
  return lessons, nil
}










