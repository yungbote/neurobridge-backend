package jobs

import (
	"context"
	"time"
	"gorm.io/gorm"
	"github.com/yungbote/neurobridge-backend/internal/logger"
	"github.com/yungbote/neurobridge-backend/internal/repos"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type Worker struct {
	db       *gorm.DB
	log      *logger.Logger
	repo     repos.JobRunRepo
	registry *Registry
	notify   services.JobNotifier
}

func NewWorker(db *gorm.DB, baseLog *logger.Logger, repo repos.JobRunRepo, registry *Registry, notify services.JobNotifier) *Worker {
	return &Worker{
		db:       db,
		log:      baseLog.With("component", "JobWorker"),
		repo:     repo,
		registry: registry,
		notify:   notify,
	}
}

func (w *Worker) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		const maxAttempts = 5
		retryDelay := 30 * time.Second
		staleRunning := 2 * time.Minute
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				job, err := w.repo.ClaimNextRunnable(ctx, w.db, maxAttempts, retryDelay, staleRunning)
				if err != nil {
					w.log.Warn("ClaimNextRunnable failed", "error", err)
					continue
				}
				if job == nil {
					continue
				}
				h, ok := w.registry.Get(job.JobType)
				if !ok {
					w.log.Warn("No handler registered for job_type", "job_type", job.JobType, "job_id", job.ID)
					jc := NewContext(ctx, w.db, job, w.repo, w.notify)
					jc.Fail("dispatch", 	// stage
						&missingHandlerError{JobType: job.JobType},
					)
					continue
				}
				jc := NewContext(ctx, w.db, job, w.repo, w.notify)
				// If handler panics, we want to mark failed.
				func() {
					defer func() {
						if r := recover(); r != nil {
							w.log.Error("Job handler panic", "job_id", job.ID, "job_type", job.JobType, "panic", r)
							jc.Fail("panic", errFromRecover(r))
						}
					}()

					h.Run(jc)
				}()
			}
		}
	}()
}

type missingHandlerError struct{ JobType string }

func (e *missingHandlerError) Error() string { return "no handler registered for job_type=" + e.JobType }

func errFromRecover(v any) error {
	return &panicError{Val: v}
}

type panicError struct{ Val any }

func (e *panicError) Error() string { return "panic: unexpected error" }










