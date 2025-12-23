package services

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type WorkflowService interface {
	UploadMaterialsAndStartLearningBuild(ctx context.Context, tx *gorm.DB, userID uuid.UUID, uploaded []UploadedFileInfo) (*types.MaterialSet, *types.JobRun, error)
	UploadMaterialsAndStartLearningBuildWithChat(ctx context.Context, tx *gorm.DB, userID uuid.UUID, uploaded []UploadedFileInfo) (*types.MaterialSet, uuid.UUID, *types.ChatThread, *types.JobRun, error)
}

type workflowService struct {
	db        *gorm.DB
	log       *logger.Logger
	materials MaterialService
	jobs      JobService

	bootstrap LearningBuildBootstrapService
	threads   repos.ChatThreadRepo
}

func NewWorkflowService(
	db *gorm.DB,
	baseLog *logger.Logger,
	materials MaterialService,
	jobs JobService,
	bootstrap LearningBuildBootstrapService,
	threads repos.ChatThreadRepo,
) WorkflowService {
	return &workflowService{
		db:        db,
		log:       baseLog.With("service", "WorkflowService"),
		materials: materials,
		jobs:      jobs,
		bootstrap: bootstrap,
		threads:   threads,
	}
}

func (w *workflowService) UploadMaterialsAndStartLearningBuild(
	ctx context.Context,
	tx *gorm.DB,
	userID uuid.UUID,
	uploaded []UploadedFileInfo,
) (*types.MaterialSet, *types.JobRun, error) {
	set, _, _, job, err := w.UploadMaterialsAndStartLearningBuildWithChat(ctx, tx, userID, uploaded)
	return set, job, err
}

func (w *workflowService) UploadMaterialsAndStartLearningBuildWithChat(
	ctx context.Context,
	tx *gorm.DB,
	userID uuid.UUID,
	uploaded []UploadedFileInfo,
) (*types.MaterialSet, uuid.UUID, *types.ChatThread, *types.JobRun, error) {
	if userID == uuid.Nil {
		return nil, uuid.Nil, nil, nil, fmt.Errorf("missing user id")
	}
	if len(uploaded) == 0 {
		return nil, uuid.Nil, nil, nil, fmt.Errorf("no files")
	}

	transaction := tx
	if transaction == nil {
		transaction = w.db
	}
	if w.bootstrap == nil || w.threads == nil {
		return nil, uuid.Nil, nil, nil, fmt.Errorf("workflow service not fully configured")
	}

	var (
		set    *types.MaterialSet
		pathID uuid.UUID
		thread *types.ChatThread
		job    *types.JobRun
	)

	err := transaction.WithContext(ctx).Transaction(func(txx *gorm.DB) error {
		// 1) Persist materials (set + file rows + upload blobs)
		createdSet, _, err := w.materials.UploadMaterialFiles(ctx, txx, userID, uploaded)
		if err != nil {
			return err
		}
		set = createdSet

		// 2) Ensure canonical path exists (race-safe via user_library_index lock).
		pid, err := w.bootstrap.EnsurePath(ctx, txx, userID, set.ID)
		if err != nil {
			return err
		}
		pathID = pid

		// 3) Create chat thread bound to this path build.
		now := time.Now().UTC()
		meta := map[string]any{
			"material_set_id": set.ID.String(),
			"path_id":         pathID.String(),
			"kind":            "path_build",
		}
		metaJSON, _ := json.Marshal(meta)

		t := &types.ChatThread{
			ID:            uuid.New(),
			UserID:        userID,
			PathID:        &pathID,
			JobID:         nil, // set after job enqueue
			Title:         "New chat",
			Status:        "active",
			Metadata:      datatypes.JSON(metaJSON),
			NextSeq:       0,
			LastMessageAt: now,
			LastViewedAt:  now,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		created, err := w.threads.Create(ctx, txx, []*types.ChatThread{t})
		if err != nil {
			return err
		}
		if len(created) == 0 || created[0] == nil || created[0].ID == uuid.Nil {
			return fmt.Errorf("failed to create chat thread")
		}
		thread = created[0]

		// 4) Enqueue learning_build (root orchestrator creates saga_id).
		payload := map[string]any{
			"material_set_id": set.ID.String(),
			"path_id":         pathID.String(),
			"thread_id":       thread.ID.String(),
		}
		entityID := set.ID
		createdJob, err := w.jobs.Enqueue(ctx, txx, userID, "learning_build", "material_set", &entityID, payload)
		if err != nil {
			return err
		}
		job = createdJob

		// 5) Backlink job onto thread (non-authoritative, but useful for UI).
		if err := w.threads.UpdateFields(ctx, txx, thread.ID, map[string]interface{}{
			"job_id": job.ID,
		}); err != nil {
			return err
		}
		thread.JobID = &job.ID

		return nil
	})
	if err != nil {
		return nil, uuid.Nil, nil, nil, err
	}

	// Keep behavior similar: return immediately; worker will run the job.
	return set, pathID, thread, job, nil
}
