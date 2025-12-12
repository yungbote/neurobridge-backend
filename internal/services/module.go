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

type ModuleService interface {
  ListModulesForCourse(ctx context.Context, tx *gorm.DB, courseID uuid.UUID) ([]*types.CourseModule, error)
}

type moduleService struct {
  db         *gorm.DB
  log        *logger.Logger
  courseRepo repos.CourseRepo
  moduleRepo repos.CourseModuleRepo
}

func NewModuleService(
  db *gorm.DB,
  baseLog *logger.Logger,
  courseRepo repos.CourseRepo,
  moduleRepo repos.CourseModuleRepo,
) ModuleService {
  return &moduleService{
    db:         db,
    log:        baseLog.With("service", "ModuleService"),
    courseRepo: courseRepo,
    moduleRepo: moduleRepo,
  }
}

func (s *moduleService) ListModulesForCourse(ctx context.Context, tx *gorm.DB, courseID uuid.UUID) ([]*types.CourseModule, error) {
  rd := requestdata.GetRequestData(ctx)
  if rd == nil || rd.UserID == uuid.Nil {
    return nil, fmt.Errorf("not authenticated")
  }
  if courseID == uuid.Nil {
    return nil, fmt.Errorf("missing course id")
  }

  transaction := tx
  if transaction == nil {
    transaction = s.db
  }

  // Ownership check: course must belong to user
  courses, err := s.courseRepo.GetByIDs(ctx, transaction, []uuid.UUID{courseID})
  if err != nil {
    s.log.Warn("ListModulesForCourse: load course failed", "error", err, "course_id", courseID)
    return nil, err
  }
  if len(courses) == 0 || courses[0] == nil || courses[0].UserID != rd.UserID {
    return nil, fmt.Errorf("course not found")
  }

  modules, err := s.moduleRepo.GetByCourseIDs(ctx, transaction, []uuid.UUID{courseID})
  if err != nil {
    s.log.Warn("ListModulesForCourse: load modules failed", "error", err, "course_id", courseID)
    return nil, err
  }
  return modules, nil
}










