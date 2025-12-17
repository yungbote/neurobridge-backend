package services

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"github.com/yungbote/neurobridge-backend/internal/logger"
	"github.com/yungbote/neurobridge-backend/internal/repos"
	"github.com/yungbote/neurobridge-backend/internal/requestdata"
	"github.com/yungbote/neurobridge-backend/internal/types"
)

type JobService interface {
	Enqueue(ctx context.Context, tx *gorm.DB, ownerUserID uuid.UUID, jobType string, entityType string, entityID *uuid.UUID, payload map[string]any) (*types.JobRun, error)
	EnqueueDebouncedUserModelUpdate(ctx context.Context, tx *gorm.DB, userID uuid.UUID) (*types.JobRun, bool, error)
	EnqueueUserModelUpdateIfNeeded(ctx context.Context, tx *gorm.DB, ownerUserID uuid.UUID, trigger string) (*types.JobRun, bool, error)
	GetByIDForRequestUser(ctx context.Context, tx *gorm.DB, jobID uuid.UUID) (*types.JobRun, error)
	GetLatestForEntityForRequestUser(ctx context.Context, tx *gorm.DB, entityType string, entityID uuid.UUID, jobType string) (*types.JobRun, error)
}

type jobService struct {
	db     *gorm.DB
	log    *logger.Logger
	repo   repos.JobRunRepo
	notify JobNotifier
}

func NewJobService(db *gorm.DB, baseLog *logger.Logger, repo repos.JobRunRepo, notify JobNotifier) JobService {
	return &jobService{
		db:     db,
		log:    baseLog.With("service", "JobService"),
		repo:   repo,
		notify: notify,
	}
}

func (s *jobService) Enqueue(ctx context.Context, tx *gorm.DB, ownerUserID uuid.UUID, jobType string, entityType string, entityID *uuid.UUID, payload map[string]any) (*types.JobRun, error) {
	if ownerUserID == uuid.Nil {
		return nil, fmt.Errorf("missing owner_user_id")
	}
	if jobType == "" {
		return nil, fmt.Errorf("missing job_type")
	}
	transaction := tx
	if transaction == nil {
		transaction = s.db
	}
	var payloadJSON datatypes.JSON
	if payload != nil {
		b, _ := json.Marshal(payload)
		payloadJSON = datatypes.JSON(b)
	} else {
		payloadJSON = datatypes.JSON([]byte(`{}`))
	}
	now := time.Now()
	job := &types.JobRun{
		ID:          uuid.New(),
		OwnerUserID: ownerUserID,
		JobType:     jobType,
		EntityType:  entityType,
		EntityID:    entityID,
		Status:      "queued",
		Stage:       "queued",
		Progress:    0,
		Attempts:    0,
		Payload:     payloadJSON,
		Result:      datatypes.JSON([]byte(`{}`)),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if _, err := s.repo.Create(ctx, transaction, []*types.JobRun{job}); err != nil {
		return nil, fmt.Errorf("create job: %w", err)
	}
	// Notify immediately (request-time)
	s.notify.JobCreated(ownerUserID, job)
	return job, nil
}

func (s *jobService) EnqueueDebouncedUserModelUpdate(ctx context.Context, tx *gorm.DB, userID uuid.UUID) (*types.JobRun, bool, error) {
	if userID == uuid.Nil {
		return nil, false, fmt.Errorf("missing user_id")
	}
	transaction := tx
	if transaction == nil {
		transaction = s.db
	}
	// If a user_model_update job is already queued/running for this user, do nothing.
	has, err := s.repo.HasRunnableForEntity(ctx, transaction, userID, "user", userID, "user_model_update")
	if err != nil {
		return nil, false, err
	}
	if has {
		return nil, false, nil
	}

	payload := map[string]any{
		"user_id": userID.String(),
	}
	entityID := userID
	job, err := s.Enqueue(ctx, transaction, userID, "user_model_update", "user", &entityID, payload)
	if err != nil {
		return nil, false, err
	}
	return job, true, nil
}

func (s *jobService) EnqueueUserModelUpdateIfNeeded(ctx context.Context, tx *gorm.DB, ownerUserID uuid.UUID, trigger string) (*types.JobRun, bool, error) {
	if ownerUserID == uuid.Nil {
		return nil, false, fmt.Errorf("missing owner_user_id")
	}
	transaction := tx
	if transaction == nil {
		transaction = s.db
	}

	entityID := ownerUserID
	exists, err := s.repo.ExistsRunnable(ctx, transaction, ownerUserID, "user_model_update", "user", &entityID)
	if err != nil {
		return nil, false, err
	}
	if exists {
		return nil, false, nil
	}

	payload := map[string]any{
		"trigger": trigger,
	}
	job, err := s.Enqueue(ctx, transaction, ownerUserID, "user_model_update", "user", &entityID, payload)
	if err != nil {
		return nil, false, err
	}
	return job, true, nil
}

func (s *jobService) GetByIDForRequestUser(ctx context.Context, tx *gorm.DB, jobID uuid.UUID) (*types.JobRun, error) {
	rd := requestdata.GetRequestData(ctx)
	if rd == nil || rd.UserID == uuid.Nil {
		return nil, fmt.Errorf("not authenticated")
	}
	if jobID == uuid.Nil {
		return nil, fmt.Errorf("missing job id")
	}
	transaction := tx
	if transaction == nil {
		transaction = s.db
	}

	rows, err := s.repo.GetByIDs(ctx, transaction, []uuid.UUID{jobID})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 || rows[0] == nil {
		return nil, fmt.Errorf("job not found")
	}
	if rows[0].OwnerUserID != rd.UserID {
		return nil, fmt.Errorf("job not found")
	}
	return rows[0], nil
}

func (s *jobService) GetLatestForEntityForRequestUser(ctx context.Context, tx *gorm.DB, entityType string, entityID uuid.UUID, jobType string) (*types.JobRun, error) {
	rd := requestdata.GetRequestData(ctx)
	if rd == nil || rd.UserID == uuid.Nil {
		return nil, fmt.Errorf("not authenticated")
	}
	if entityType == "" || entityID == uuid.Nil || jobType == "" {
		return nil, fmt.Errorf("missing entity/job info")
	}
	transaction := tx
	if transaction == nil {
		transaction = s.db
	}
	return s.repo.GetLatestByEntity(ctx, transaction, rd.UserID, entityType, entityID, jobType)
}










