package worker

import (
	"context"
	"os"
	"strconv"
	"time"

	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/logger"
	"github.com/yungbote/neurobridge-backend/internal/repos"
	"github.com/yungbote/neurobridge-backend/internal/services"
	"github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
)

type Worker struct {
	db       *gorm.DB
	log      *logger.Logger
	repo     repos.JobRunRepo
	registry *runtime.Registry
	notify   services.JobNotifier
}

func NewWorker(db *gorm.DB, baseLog *logger.Logger, repo repos.JobRunRepo, registry *runtime.Registry, notify services.JobNotifier) *Worker {
	return &Worker{
		db:       db,
		log:      baseLog.With("component", "JobWorker"),
		repo:     repo,
		registry: registry,
		notify:   notify,
	}
}

func (w *Worker) Start(ctx context.Context) {
	concurrency := getEnvInt("WORKER_CONCURRENCY", 4)
	if concurrency < 1 {
		concurrency = 1
	}
	w.log.Info("Starting job worker pool", "concurrency", concurrency)

	for i := 0; i < concurrency; i++ {
		workerID := i + 1
		go w.runLoop(ctx, workerID)
	}
}

func (w *Worker) runLoop(ctx context.Context, workerID int) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	const maxAttempts = 5
	retryDelay := 30 * time.Second
	staleRunning := 30 * time.Minute

	for {
		select {
		case <-ctx.Done():
			w.log.Info("Worker loop stopped", "worker_id", workerID)
			return
		case <-ticker.C:
			job, err := w.repo.ClaimNextRunnable(ctx, w.db, maxAttempts, retryDelay, staleRunning)
			if err != nil {
				w.log.Warn("ClaimNextRunnable failed", "worker_id", workerID, "error", err)
				continue
			}
			if job == nil {
				continue
			}

			h, ok := w.registry.Get(job.JobType)
			jc := runtime.NewContext(ctx, w.db, job, w.repo, w.notify)

			if !ok {
				w.log.Warn("No handler registered for job_type",
					"worker_id", workerID,
					"job_type", job.JobType,
					"job_id", job.ID,
				)
				jc.Fail("dispatch", &missingHandlerError{JobType: job.JobType})
				continue
			}

			func() {
				defer func() {
					if r := recover(); r != nil {
						w.log.Error("Job handler panic",
							"worker_id", workerID,
							"job_id", job.ID,
							"job_type", job.JobType,
							"panic", r,
						)
						jc.Fail("panic", errFromRecover(r))
					}
				}()

				if runErr := h.Run(jc); runErr != nil {
					// Most pipelines call jc.Fail themselves; this is a safety net.
					jc.Fail("run", runErr)
				}
			}()
		}
	}
}

type missingHandlerError struct{ JobType string }

func (e *missingHandlerError) Error() string { return "no handler registered for job_type=" + e.JobType }

func errFromRecover(v any) error { return &panicError{Val: v} }

type panicError struct{ Val any }

func (e *panicError) Error() string { return "panic: unexpected error" }

func getEnvInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return i
}










