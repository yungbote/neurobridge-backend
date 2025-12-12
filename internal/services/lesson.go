package services

import (
	"context"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"github.com/yungbote/neurbridge-backend/internal/types"
)

type LessonService interface {
	GetLessonForUser(ctx context.Context, userID, lessonID uuid.UUID) (*types.Lesson, []*types.LessonAsset, error)
	ListLessonsForCourse(ctx context.Context, userID, courseID uuid.UUID) ([]*types.Lesson, error)
	UpdateLesson(ctx context.Context, userID, lessonID uuid.UUID, updates map[string]interface{}) (*types.Lesson, error)
	GetLessonHistory(ctx context.Context, userID, lessonID uuid.UUID) ([]map[string]interface{}, error)
	ReorderLessons(ctx context.Context, userID, courseID uuid.UUID, newOrder map[uuid.UUID]int) error
}










