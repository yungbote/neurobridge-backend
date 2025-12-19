package services

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type WorkflowService interface {
	UploadMaterialsAndStartLearningBuild(ctx context.Context, tx *gorm.DB, userID uuid.UUID, uploaded []UploadedFileInfo) (*types.MaterialSet, *types.JobRun, error)
}

type workflowService struct {
	db        *gorm.DB
	log       *logger.Logger
	materials MaterialService
	jobs      JobService
}

func NewWorkflowService(
	db *gorm.DB,
	baseLog *logger.Logger,
	materials MaterialService,
	jobs JobService,
) WorkflowService {
	return &workflowService{
		db:        db,
		log:       baseLog.With("service", "WorkflowService"),
		materials: materials,
		jobs:      jobs,
	}
}

func (w *workflowService) UploadMaterialsAndStartLearningBuild(
	ctx context.Context,
	tx *gorm.DB,
	userID uuid.UUID,
	uploaded []UploadedFileInfo,
) (*types.MaterialSet, *types.JobRun, error) {
	if userID == uuid.Nil {
		return nil, nil, fmt.Errorf("missing user id")
	}
	if len(uploaded) == 0 {
		return nil, nil, fmt.Errorf("no files")
	}

	transaction := tx
	if transaction == nil {
		transaction = w.db
	}

	var (
		set *types.MaterialSet
		job *types.JobRun
	)

	err := transaction.WithContext(ctx).Transaction(func(txx *gorm.DB) error {
		// 1) Persist materials (set + file rows + upload blobs)
		createdSet, _, err := w.materials.UploadMaterialFiles(ctx, txx, userID, uploaded)
		if err != nil {
			return err
		}
		set = createdSet

		// 2) Enqueue learning_build (root orchestrator creates saga_id).
		payload := map[string]any{
			"material_set_id": set.ID.String(),
		}
		entityID := set.ID
		createdJob, err := w.jobs.Enqueue(ctx, txx, userID, "learning_build", "material_set", &entityID, payload)
		if err != nil {
			return err
		}
		job = createdJob

		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	// Keep behavior similar: return immediately; worker will run the job.
	return set, job, nil
}
