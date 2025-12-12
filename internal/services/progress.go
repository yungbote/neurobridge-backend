package services

import (
	"context"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"github.com/yungbote/neurbridge-backend/internal/types"
)

type ProgressService interface {
	RecordLessonEvent(ctx context.Context, userID, lessonID uuid.UUID, eventType string, data map[string]interface{}) error
	GetLessonProgressForUser(ctx context.Context, userID uuid.UUID, lessonIDs []uuid.UUID) ([]*types.LessonProgress, error)
}










