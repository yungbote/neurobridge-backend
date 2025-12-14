package jobs

import (
	"context"
	"time"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"github.com/yungbote/neurobridge-backend/internal/types"
)

type RunnablePolicy struct {
	MaxAttempts				int
	RetryDelay				time.Duration
	StaleRunning			time.Duration
}

// JobStore is the generic runtime interface
// Right now, it is backed by the existing course_generation_run table
type JobStore interface {
	// Creates a new job row
	Enqueue(ctx context.Context, tx *gorm.DB, run *types.CourseGenerationRun) (*types.CourseGenerationRun, error)
	// Picks on runnable job and marks it running (SKIP LOCKED)
	ClaimNextRunnable(ctx context.Context, tx *gorm.DB, policy RunnablePolicy) (*types.CourseGenerationRun, error)
	// Updates a job row by id
	UpdateFields(ctx context.Context, tx *gorm.DB, id uuid.UUID, updates map[string]any) error
}










