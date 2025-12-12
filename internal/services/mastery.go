package services

import (
	"context"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"github.com/yungbote/neurobridge-backend/internal/types"
)

type MasterService interface {
	GetTopicMasteryForUser(ctx context.Context, userID uuid.UUID, filters map[string]interface{}) ([]*types.TopicMastery, error)
	UpdateMasteryForUser(ctx context.Context, userID uuid.UUID, topic string, delta float64, metadata map[string]interface{}) error
	BulkUpdateMastery(ctx context.Context, userID uuid.UUID, updates map[string]float64) error
}










