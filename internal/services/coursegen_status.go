package services

import (
  "context"
  "fmt"

  "github.com/google/uuid"
  "gorm.io/gorm"

  "github.com/yungbote/neurobridge-backend/internal/repos"
  "github.com/yungbote/neurobridge-backend/internal/requestdata"
  "github.com/yungbote/neurobridge-backend/internal/types"
)

type CourseGenStatusService interface {
  GetLatestRunForCourse(ctx context.Context, tx *gorm.DB, courseID uuid.UUID) (*types.CourseGenerationRun, error)
  GetRunByID(ctx context.Context, tx *gorm.DB, runID uuid.UUID) (*types.CourseGenerationRun, error)
}

type courseGenStatusService struct {
  db      *gorm.DB
  runRepo repos.CourseGenerationRunRepo
  courseRepo repos.CourseRepo
}

func NewCourseGenStatusService(db *gorm.DB, runRepo repos.CourseGenerationRunRepo, courseRepo repos.CourseRepo) CourseGenStatusService {
  return &courseGenStatusService{
    db: db,
    runRepo: runRepo,
    courseRepo: courseRepo,
  }
}

func (s *courseGenStatusService) GetLatestRunForCourse(ctx context.Context, tx *gorm.DB, courseID uuid.UUID) (*types.CourseGenerationRun, error) {
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

  // authorize: course must belong to user
  courses, err := s.courseRepo.GetByIDs(ctx, transaction, []uuid.UUID{courseID})
  if err != nil {
    return nil, err
  }
  if len(courses) == 0 || courses[0] == nil || courses[0].UserID != rd.UserID {
    return nil, fmt.Errorf("course not found")
  }

  run, err := s.runRepo.GetLatestByCourseID(ctx, transaction, courseID)
  if err != nil {
    return nil, err
  }
  return run, nil
}

func (s *courseGenStatusService) GetRunByID(ctx context.Context, tx *gorm.DB, runID uuid.UUID) (*types.CourseGenerationRun, error) {
  rd := requestdata.GetRequestData(ctx)
  if rd == nil || rd.UserID == uuid.Nil {
    return nil, fmt.Errorf("not authenticated")
  }
  if runID == uuid.Nil {
    return nil, fmt.Errorf("missing run id")
  }

  transaction := tx
  if transaction == nil {
    transaction = s.db
  }

  runs, err := s.runRepo.GetByIDs(ctx, transaction, []uuid.UUID{runID})
  if err != nil {
    return nil, err
  }
  if len(runs) == 0 || runs[0] == nil {
    return nil, fmt.Errorf("run not found")
  }
  if runs[0].UserID != rd.UserID {
    return nil, fmt.Errorf("run not found")
  }
  return runs[0], nil
}










