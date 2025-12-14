package jobs

import (
	"context"
	"fmt"
	"time"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/datatypes"
	"github.com/yungbote/neurobridge-backend/internal/logger"
	"github.com/yungbote/neurobridge-backend/internal/repos"
	"github.com/yungbote/neurobridge-backend/internal/types"
)

type CourseGenerationRunStore struct {
	db				*gorm.DB
	log				*logger.Logger
	repo			repos.CourseGenerationRunRepo
}

func NewCourseGenerationRunStore(db *gorm.DB, baseLog *logger.Logger, repo repos.CourseGenerationRunRepo) *CourseGenerationRunStore {
	return &CourseGenerationRunStore{
		db:		db,
		log:	baseLog.With("component", "JobStore"),
		repo: repo,
	}
}


func (s *CourseGenerationRunStore) Enqueue(ctx context.Context, tx *gorm.DB, run *types.CourseGenerationRun) (*types.CourseGenerationRun, error) {
	if run == nil {
		return nil, fmt.Errorf("nil run")
	}
	transaction := tx
	if transaction == nil {
		transaction = s.db
	}
	if run.ID == uuid.Nil {
		run.ID = uuid.New()
	}
	if run.Status == "" {
		run.Status = "queued"
	}
	if run.Stage == "" {
		run.Stage = "ingest"
	}
	if run.Metadata == nil {
		run.Metadata = datatypes.JSON([]byte(`{}`))
	}
	now := time.Now()
	if run.CreatedAt.IsZero() {
		run.CreatedAt = now
	}
	run.UpdatedAt = now
	created, err := s.repo.Create(ctx, transaction, []*types.CourseGenerationRun{run})
	if err != nil {
		return nil, err
	}
	if len(created) == 0 || created[0] == nil {
		return nil, fmt.Errorf("failed to enqueue job")
	}
	return created[0], nil
}

func (s *CourseGenerationRunStore) ClaimNextRunnable(ctx context.Context, tx *gorm.DB, policy RunnablePolicy) (*types.CourseGenerationRun, error) {
	transaction := tx
	if transaction == nil {
		transaction = s.db
	}
	if policy.MaxAttempts <= 0 {
		policy.MaxAttempts = 5
	}
	if policy.RetryDelay <= 0 {
		policy.RetryDelay = 30 * time.Second
	}
	if policy.StaleRunning <= 0 {
		policy.StaleRunning = 2 * time.Minute
	}
	return s.repo.ClaimNextRunnable(ctx, transaction, policy.MaxAttempts, policy.RetryDelay, policy.StaleRunning)
}

func (s *CourseGenerationRunStore) UpdateFields(ctx context.Context, tx *gorm.DB, id uuid.UUID, updates map[string]any) error {
	transaction := tx
	if transaction == nil {
		transaction = s.db
	}
	return s.repo.UpdateFields(ctx, transaction, id, updates)
}










