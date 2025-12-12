package services

import (
	"context"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"github.com/yungbote/neurbridge-backend/internal/types"
)

type AdaptationService interface {
  GetNextLessonForUser(ctx context.Context, userID uuid.UUID, courseID *uuid.UUID) (*types.Lesson, error)
  GetAdaptedLessonView(ctx context.Context, userID, lessonID uuid.UUID) (map[string]interface{}, error)
  GetPracticeSet(ctx context.Context, userID uuid.UUID, params map[string]interface{}) ([]*types.Lesson, error)
  GetReviewPlan(ctx context.Context, userID uuid.UUID, params map[string]interface{}) (map[string]interface{}, error)
  PreviewAdaptation(ctx context.Context, userID uuid.UUID, hypotheticalProfile map[string]interface{}) (map[string]interface{}, error)
}










