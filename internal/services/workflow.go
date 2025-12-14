package services

import (
	"context"
	"fmt"
	"time"
	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"github.com/yungbote/neurobridge-backend/internal/logger"
	"github.com/yungbote/neurobridge-backend/internal/repos"
	"github.com/yungbote/neurobridge-backend/internal/types"
)

type WorkflowService interface {
	UploadMaterialsAndStartCourseBuild(ctx context.Context, tx *gorm.DB, userID uuid.UUID, uploaded []UploadedFileInfo) (*types.MaterialSet, *types.Course, *types.JobRun, error)
}

type workflowService struct {
	db *gorm.DB
	log *logger.Logger
	materials MaterialService
	courseRepo repos.CourseRepo
	jobs JobService
}

func NewWorkflowService(
	db *gorm.DB,
	baseLog *logger.Logger,
	materials MaterialService,
	courseRepo repos.CourseRepo,
	jobs JobService,
) WorkflowService {
	return &workflowService{
		db: db,
		log: baseLog.With("service", "WorkflowService"),
		materials: materials,
		courseRepo: courseRepo,
		jobs: jobs,
	}
}

func (w *workflowService) UploadMaterialsAndStartCourseBuild(
	ctx context.Context,
	tx *gorm.DB,
	userID uuid.UUID,
	uploaded []UploadedFileInfo,
) (*types.MaterialSet, *types.Course, *types.JobRun, error) {
	if userID == uuid.Nil {
		return nil, nil, nil, fmt.Errorf("missing user id")
	}
	if len(uploaded) == 0 {
		return nil, nil, nil, fmt.Errorf("no files")
	}
	transaction := tx
	if transaction == nil {
		transaction = w.db
	}
	var (
		set *types.MaterialSet
		course *types.Course
		job *types.JobRun
	)
	err := transaction.WithContext(ctx).Transaction(func(txx *gorm.DB) error {
		// 1) Persist materials (set + file rows + upload blobs)
		createdSet, _, err := w.materials.UploadMaterialFiles(ctx, txx, userID, uploaded)
		if err != nil {
			return err
		}
		set = createdSet
		// 2) Create placeholder course row (domain object)
		now := time.Now()
		course = &types.Course{
			ID:            uuid.New(),
			UserID:        userID,
			MaterialSetID: &set.ID,
			Title:         "Generating course…",
			Description:   "We’re analyzing your files and building your course.",
			Metadata:      datatypes.JSON([]byte(`{"status":"generating"}`)),
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		if _, err := w.courseRepo.Create(ctx, txx, []*types.Course{course}); err != nil {
			return fmt.Errorf("create course: %w", err)
		}
		// 3) Enqueue generic job pointing at this course
		payload := map[string]any{
			"material_set_id": set.ID.String(),
			"course_id":       course.ID.String(),
		}
		entityID := course.ID
		createdJob, err := w.jobs.Enqueue(ctx, txx, userID, "course_build", "course", &entityID, payload)
		if err != nil {
			return err
		}
		job = createdJob
		return nil
	})
	if err != nil {
		return nil, nil, nil, err
	}
	return set, course, job, nil
}










