package services

import (
  "context"
  "fmt"
  "math/rand"
  "strings"
  "time"

  "github.com/google/uuid"
  "gorm.io/gorm"

  "github.com/yungbote/neurobridge-backend/internal/logger"
  "github.com/yungbote/neurobridge-backend/internal/repos"
  "github.com/yungbote/neurobridge-backend/internal/types"
  "github.com/yungbote/neurobridge-backend/internal/requestdata"
  "github.com/yungbote/neurobridge-backend/internal/ssedata"
  "github.com/yungbote/neurobridge-backend/internal/sse"
)

type CourseService interface {
  CreateCourseFromMaterialSet(ctx context.Context, tx *gorm.DB, userID, materialSetID uuid.UUID) (*types.Course, error)
  // GET
  GetUserCourses(ctx context.Context, tx *gorm.DB) ([]*types.Course, error)
}

type courseService struct {
  db              *gorm.DB
  log             *logger.Logger
  courseRepo      repos.CourseRepo
  materialSetRepo repos.MaterialSetRepo
}

func NewCourseService(
  db *gorm.DB,
  baseLog *logger.Logger,
  courseRepo repos.CourseRepo,
  materialSetRepo repos.MaterialSetRepo,
) CourseService {
  serviceLog := baseLog.With("service", "CourseService")
  rand.Seed(time.Now().UnixNano())
  return &courseService{
    db:              db,
    log:             serviceLog,
    courseRepo:      courseRepo,
    materialSetRepo: materialSetRepo,
  }
}

func (cs *courseService) CreateCourseFromMaterialSet(
  ctx context.Context,
  tx *gorm.DB,
  userID, materialSetID uuid.UUID,
) (*types.Course, error) {
  // Resolve transaction
  transaction := tx
  if transaction == nil {
    transaction = cs.db
  }

  // 1) Ensure material set belongs to this user
  sets, err := cs.materialSetRepo.GetByIDs(ctx, transaction, []uuid.UUID{materialSetID})
  if err != nil {
    return nil, fmt.Errorf("load material set: %w", err)
  }
  if len(sets) == 0 || sets[0] == nil || sets[0].UserID != userID {
    return nil, fmt.Errorf("material set not found or not owned by user")
  }

  // 2) Random-ish title/description for now
  title := randomCourseTitle()
  description := randomCourseDescription()

  now := time.Now()
  course := &types.Course{
    ID:            uuid.New(),
    UserID:        userID,
    MaterialSetID: &materialSetID,
    Title:         title,
    Description:   description,
    CreatedAt:     now,
    UpdatedAt:     now,
  }

  if _, err := cs.courseRepo.Create(ctx, transaction, []*types.Course{course}); err != nil {
    cs.log.Error("CreateCourseFromMaterialSet failed", "error", err)
    return nil, fmt.Errorf("create course: %w", err)
  }

  // 3) SSE: queue a CourseCreated event on the user channel
  if ssd := ssedata.GetSSEData(ctx); ssd != nil {
    ssd.AppendMessage(sse.SSEMessage{
      Channel: userID.String(),           // user-specific channel
      Event:   sse.SSEEventUserCourseCreated, // the new event type
      Data: map[string]interface{}{
        "course": course,
      },
    })
  } else {
    cs.log.Debug("No SSEData in context; skipping CourseCreated SSE append")
  }

  return course, nil
}

// random helpers, like your example
func randomCourseTitle() string {
  adjectives := []string{"Foundations", "Essentials", "Deep Dive", "Primer", "Overview", "Bootcamp"}
  topics := []string{"Concepts", "Workflow", "Toolkit", "System", "Materials", "Knowledge"}
  a := adjectives[rand.Intn(len(adjectives))]
  t := topics[rand.Intn(len(topics))]

  return strings.TrimSpace(fmt.Sprintf("%s %s", a, t))
}

func randomCourseDescription() string {
  templates := []string{
    "A generated course scaffold based on your uploaded materials.",
    "An initial course shell created from your latest upload.",
    "A quick starting point derived from your files.",
    "A course scaffold that will be refined as you add more context.",
  }
  return templates[rand.Intn(len(templates))]
}

func (cs *courseService) GetUserCourses(ctx context.Context, tx *gorm.DB) ([]*types.Course, error) {
  rd := requestdata.GetRequestData(ctx)
  if rd == nil {
    cs.log.Warn("Request data not set in context")
    return nil, fmt.Errorf("Request data not set in context")
  }
  if rd.UserID == uuid.Nil {
    cs.log.Warn("User id not set in request data")
    return nil, fmt.Errorf("User id not set in request data")
  }
  transaction := tx
  if transaction == nil {
    transaction = cs.db
  }
  courses, err := cs.courseRepo.GetByUserIDs(ctx, transaction, []uuid.UUID{rd.UserID})
  if err != nil {
    cs.log.Error("GetUserCourses failed", "error", err, "user_id", rd.UserID)
    return nil, fmt.Errorf("get user courses: %w", err)
  }
  return courses, nil
}










