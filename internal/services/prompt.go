package services

import (
  "context"
  "encoding/json"
  "fmt"
  "github.com/google/uuid"
  "github.com/yungbote/neurobridge-backend/internal/logger"
)

type PromptService interface {
  GenerateCourseBlueprint(ctx context.Context, userID, materialSetID uuid.UUID, input CourseBlueprintInput) (*CourseBlueprintOutput, error)
  GenerateLessonContent(ctx context.Context, userID, lessonID uuid.UUID, input LessonContentInput) (*LessonContentOutput, error)
  GenerateQuizForLesson(ctx context.Context, userID, lessonID uuid.UUID, input QuizGenerationInput) (*QuizGenerationOutput, error)
  InferLearningProfile(ctx context.Context, userID uuid.UUID, events []TelemetryEvent, baselineProfile map[string]interface{}) (map[string]interface{}, error)
}

type promptService struct {
  log		*logger.Logger
  aiClient	AIClient
}

func NewPromptService(log *logger.Logger, ai AIClient) PromptService {
  return &promptService{
    log:	log.With("service", "PromptService"),
    aiClient:	ai,
  }
}

func mustJSON(v any) string {

}










