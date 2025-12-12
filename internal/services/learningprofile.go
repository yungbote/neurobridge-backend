package services

import (
	"context"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"github.com/yungbote/neurobridge-backend/internal/types"
)

type LearningProfileService interface {
	GetByUserID(ctx context.Context, userID uuid.UUID) (*types.LearningProfile, error)
	UpsertForUser(ctx context.Context, userID uuid.UUID, body map[string]interface{}) error
	InferFromTelemetry(ctx context.Context, userID uuid.UUID) (*types.LearningProfile, error)
	SuggestAdjustments(ctx context.Context, userID uuid.UUID) (map[string]interface{}, error)
}










