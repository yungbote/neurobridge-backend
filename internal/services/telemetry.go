package services

import (
	"context"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"github.com/yungbote/neurbridge-backend/internal/types"
)

type TelemetryService interface {
  RecordLessonEvent(ctx context.Context, userID, lessonID uuid.UUID, eventType string, data map[string]interface{}) error
  RecordQuizAttempt(ctx context.Context, userID, lessonID, questionID uuid.UUID, isCorrect bool, rawAnswer map[string]interface{}, metadata map[string]interface{}) error
  RecordFeedback(ctx context.Context, userID uuid.UUID, lessonID *uuid.UUID, feedbackType string, data map[string]interface{}) error
  RecordBatch(ctx context.Context, userID uuid.UUID, events []TelemetryEvent) error
}

type TelemetryEvent struct {
  Type     string
  LessonID *uuid.UUID
  CourseID *uuid.UUID
  Data     map[string]interface{}
}










