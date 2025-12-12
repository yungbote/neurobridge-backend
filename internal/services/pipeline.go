package services

import (
	"context"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"github.com/yungbote/neurbridge-backend/internal/types"
)

type PipelineService interface {
	RunFull(ctx context.Context, userID, materialSetID uuid.UUID) (*types.Course, error)
	AnalyzeMaterialSet(ctx context.Context, materialSetID uuid.UUID) error
	PlanCourse(ctx context.Context, materialSetID, userID uuid.UUID) (*types.CourseBlueprint, error)
	GenerateCourse(ctx context.Context, blueprintID uuid.UUID) (*types.Course, error)
	GenerateLessonsForCourse(ctx context.Context, courseID, userID uuid.UUID) error
	RegenerateLesson(ctx context.Context, lessonID, userID uuid.UUID) error
	GetPipelineStatus(ctx context.Context, materialSetID uuid.UUID) (map[string]interface{}, error)
	CancelPipeline(ctx context.Context, materialSetID uuid.UUID) error
}










