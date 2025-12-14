package jobs

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"github.com/yungbote/neurobridge-backend/internal/repos"
	"github.com/yungbote/neurobridge-backend/internal/requestdata"
	"github.com/yungbote/neurobridge-backend/internal/types"
)

type StatusService interface {
	GetLatestForCourse(ctx context.Context, tx *gorm.DB, courseID uuid.UUID) (*types.CourseGenerationRun, error)
	GetByID(ctx context.Context, tx *gorm.DB, runID uuid.UUID) (*types.CourseGenerationRun, error)
}

type statusService struct {
	db						*gorm.DB
	runRepo				repos.CourseGenerationRunRepo
	courseRepo		repos.CourseRepo
}










