package services

import (
  "context"
  "github.com/google/uuid"
  "gorm.io/gorm"
  "github.com/yungbote/neurbridge-backend/internal/types"
)


type RecommendationService interface {
  GetNextSteps(ctx context.Context, userID uuid.UUID) ([]map[string]interface{}, error)
  GetReviewItems(ctx context.Context, userID uuid.UUID) ([]map[string]interface{}, error)
  GetRecommendedResources(ctx context.Context, userID uuid.UUID) ([]map[string]interface{}, error)
}










