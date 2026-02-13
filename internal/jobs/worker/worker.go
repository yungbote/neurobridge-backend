package worker

import (
	"context"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/rollback"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

/*
Job worker is the execution engine for the SQL-backed job queue.

High-level responsiblities:
  - Poll the job_run table for runnable jobs (via JobRunRepo.ClaimNextRunnable)
  - Claim a job with a DB-level lock/lease so only one worker runs it at a time
  - Dispatch the job to a handler registered by job_type (runtime.Registry)
  - Wrap handler execution with:
  - heartbeats (stale-running detection)
  - panic recovery (fail the job instead of crashing the worker)
  - a safety-net error -> Fail (pipelines usually Fail themselves)

Idea:

	The worker is infrastructure. It should know nothing of business logic.
	All business logic lives in job handlers (pipelines), which only interact through runtime.Context.

Concurrency:
  - Start() spawns N goroutines
  - Each goroutine runs runLoop() forever
  - The DB claim operation (ClaimNextRunnable) prevents double execution across goroutines/processes

Heartbeats:
  - Long jobs must update a heartbeat so they are not considered stuck
  - If the process dies, the heartbeat stops and the job becomes reclaimable after a stale threshold

locks are cleared on completion:
  - Both success and failure release the lease (locked_at=nil), allowing retries or new jobs to proceed

worker ticks every second:
  - Small polling interval keeps latency low for queued jobs without busy spinning
*/
type Worker struct {
	db       *gorm.DB             // DB handle used by repo methods
	log      *logger.Logger       // structured logging
	repo     repos.JobRunRepo     // scheduler + state writer for job_run rows
	registry *runtime.Registry    // job_type -> handler mapping
	notify   services.JobNotifier // side channel events (SSE)
}

/*
NewWorker wires the worker with its infrastructure dependencies.
This is intentionally thin wiring:
  - repo decides what's runnable and manages claims/leases
  - registry decides which handler runs a job_type
  - notify emits progress/done/failed events
*/
func NewWorker(db *gorm.DB, baseLog *logger.Logger, repo repos.JobRunRepo, registry *runtime.Registry, notify services.JobNotifier) *Worker {
	return &Worker{
		db:       db,
		log:      baseLog.With("component", "JobWorker"),
		repo:     repo,
		registry: registry,
		notify:   notify,
	}
}

/*
Start launches the worker pool.
It reads WORKER_CONCURRENCY (default 4) and spawns that many goroutines.
Each goroutine ryuns an independent runLoop() that claims and executes jobs.

Invariant:

	Even with many goroutines, a given job should only be executed by one worker at a time,
	enforced by the repo's claim/lease mechanism.
*/
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

/*
runLoop is the core scheduler loop.
Behavior:
  - Every tick, try to claim a runnable job (ClaimNextRunnable).
  - If none exists, do nothing and wait for the next tick.
  - If claimed, dispatch to the handler registered for job.JobType.
  - Wrap execution with:
  - heartbeat goroutine *updates job heartbeat periodically*
  - panic recovery *convert panic to job failure*
  - safety net: if handler returns error, mark failed

Retry semantics:
  - The worker does not 'retry' by re-calling the handler in process
  - Instead, a failed job remains in DB with attempts/last_error timestamps,
    and the claim query decides when it is runnable again based on retryDelay/maxAttempts
  - This makes retries durable across process restarts
*/
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
			job, err := w.repo.ClaimNextRunnable(dbctx.Context{Ctx: ctx, Tx: w.db}, maxAttempts, retryDelay, staleRunning)
			if err != nil {
				w.log.Warn("ClaimNextRunnable failed", "worker_id", workerID, "error", err)
				continue
			}
			if job == nil {
				continue
			}

			h, ok := w.registry.Get(job.JobType)
			jc := runtime.NewContext(ctx, w.db, job, w.repo, w.notify)

			if rollback.BlockedJobType(job.JobType) {
				if freeze, err := rollback.FreezeActive(ctx, w.db); err == nil && freeze.Active {
					rollback.PauseJob(ctx, w.repo, job, "Paused: structural rollback active")
					w.log.Info("Job paused for structural rollback",
						"worker_id", workerID,
						"job_id", job.ID,
						"job_type", job.JobType,
						"rollback_event_id", freeze.EventID,
						"graph_version_from", freeze.From,
						"graph_version_to", freeze.To,
					)
					continue
				}
			}

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
				stopHB := w.startHeartbeat(ctx, job.ID)
				defer stopHB()

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

/*
startHeartbeat spawns a goroutine that periodically updates job_run.heartbeat_at.
Purpose:
  - Prevent false 'stale running' detection for long-running handlers
  - Allow stuck jobs to be reclaimed if the process crashes (heartbeats stop)

returns a stop function that must be called to terminate the heartbeat goroutine.
*/
func (w *Worker) startHeartbeat(ctx context.Context, jobID uuid.UUID) func() {
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-t.C:
				if w == nil || w.repo == nil || w.db == nil || jobID == uuid.Nil {
					continue
				}
				_ = w.repo.Heartbeat(dbctx.Context{Ctx: ctx, Tx: w.db}, jobID)
			}
		}
	}()
	return func() { close(done) }
}

/*
missingHandlerError is used when a job is claimed but no handler exists for job_type.
This is usually a wiring/config issue: the job_type was enqueued but not registered.
*/
type missingHandlerError struct{ JobType string }

/*
Error formats the missing handler error in a stable, searchable way.
*/
func (e *missingHandlerError) Error() string {
	return "no handler registered for job_type=" + e.JobType
}

/*
errFromRecover converts a panic payload into an error so it can be stored in job_run.error.
*/
func errFromRecover(v any) error { return &panicError{Val: v} }

/*
panicError is a minimal wrapper that marks an execution as failed due to panic.
We intentionally avoid leaking panic internals (which may contain sensitive values).
*/
type panicError struct{ Val any }

/*
Error is intentionally generic; the real panic value is logged separately in worker logs.
*/
func (e *panicError) Error() string { return "panic: unexpected error" }

/*
getEnvInt reads an integer env var with fallback.
Used for worker-level knobs such as WORKER_CONCURRENCY.
Parsing failures fall back to default to keep the worker robust.
*/
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
