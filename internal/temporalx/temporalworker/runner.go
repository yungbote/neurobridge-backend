package temporalworker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	jobrt "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
	"github.com/yungbote/neurobridge-backend/internal/temporalx"
	"github.com/yungbote/neurobridge-backend/internal/temporalx/jobrun"
	"github.com/yungbote/neurobridge-backend/internal/utils"

	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/activity"
	temporalsdkclient "go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

type Runner struct {
	log *logger.Logger

	tc       temporalsdkclient.Client
	db       *gorm.DB
	jobRepo  repos.JobRunRepo
	registry *jobrt.Registry
	notify   services.JobNotifier
}

func NewRunner(
	log *logger.Logger,
	tc temporalsdkclient.Client,
	db *gorm.DB,
	jobRepo repos.JobRunRepo,
	registry *jobrt.Registry,
	notify services.JobNotifier,
) (*Runner, error) {
	if tc == nil {
		return nil, fmt.Errorf("temporal client is not configured")
	}
	if db == nil || jobRepo == nil || registry == nil {
		return nil, fmt.Errorf("temporal worker missing deps")
	}
	return &Runner{
		log:      log,
		tc:       tc,
		db:       db,
		jobRepo:  jobRepo,
		registry: registry,
		notify:   notify,
	}, nil
}

func (r *Runner) Start(ctx context.Context) error {
	if r == nil || r.tc == nil {
		return fmt.Errorf("temporal worker not initialized")
	}

	cfg := temporalx.LoadConfig()
	if r.log != nil {
		r.log.Info("Starting Temporal worker", "address", cfg.Address, "namespace", cfg.Namespace, "task_queue", cfg.TaskQueue)
	}

	// Local/self-hosted convenience: ensure namespace exists before polling.
	// Temporal Cloud namespaces should be pre-created and TEMPORAL_AUTO_REGISTER_NAMESPACE should be false.
	if envTrue("TEMPORAL_AUTO_REGISTER_NAMESPACE", false) {
		baseCtx := ctx
		if baseCtx == nil {
			baseCtx = context.Background()
		}
		if err := temporalx.EnsureNamespace(baseCtx, r.tc, cfg.Namespace, r.log); err != nil && r.log != nil {
			r.log.Warn("Temporal namespace ensure failed; worker will retry on start", "namespace", cfg.Namespace, "error", err)
		}
	}

	maxWait := durationSecondsFromEnv("TEMPORAL_WORKER_START_MAX_WAIT_SECONDS", 60)
	backoff := durationMillisFromEnv("TEMPORAL_WORKER_START_BACKOFF_MS", 250)
	backoffMax := durationMillisFromEnv("TEMPORAL_WORKER_START_BACKOFF_MAX_MS", 5000)

	deadline := time.Now().Add(maxWait)

	for attempt := 1; ; attempt++ {
		if ctx != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
		}

		w, err := r.newWorker()
		if err != nil {
			return err
		}
		startErr := w.Start()
		if startErr == nil {
			if ctx != nil {
				go func() {
					<-ctx.Done()
					w.Stop()
				}()
			}
			if r.log != nil {
				r.log.Info("Temporal worker started", "namespace", cfg.Namespace, "task_queue", cfg.TaskQueue, "attempts", attempt)
			}
			return nil
		}

		// Defensive: ensure worker goroutines are stopped before we retry.
		w.Stop()

		// If the namespace is missing and auto-register is enabled, try to create it then retry.
		var nfe *serviceerror.NamespaceNotFound
		if errors.As(startErr, &nfe) && envTrue("TEMPORAL_AUTO_REGISTER_NAMESPACE", false) {
			baseCtx := ctx
			if baseCtx == nil {
				baseCtx = context.Background()
			}
			if err := temporalx.EnsureNamespace(baseCtx, r.tc, cfg.Namespace, r.log); err == nil {
				// Continue to retry worker start.
			}
		}

		if maxWait <= 0 || time.Now().After(deadline) {
			// Temporal Cloud / misconfig: missing namespace will never heal without config changes.
			var nfe2 *serviceerror.NamespaceNotFound
			if errors.As(startErr, &nfe2) {
				return fmt.Errorf("temporal namespace not found (namespace=%s): %w", cfg.Namespace, startErr)
			}
			return startErr
		}

		if r.log != nil {
			r.log.Warn("Temporal worker failed to start; retrying", "namespace", cfg.Namespace, "task_queue", cfg.TaskQueue, "attempt", attempt, "error", startErr)
		}

		sleep := clampBackoff(backoff, backoffMax, attempt)
		if sleep > 0 {
			time.Sleep(sleep)
		}
	}
}

func (r *Runner) newWorker() (worker.Worker, error) {
	if r == nil || r.tc == nil {
		return nil, fmt.Errorf("temporal worker not initialized")
	}
	cfg := temporalx.LoadConfig()

	concurrency := utils.GetEnvAsInt("WORKER_CONCURRENCY", 4, r.log)
	if concurrency < 1 {
		concurrency = 1
	}

	w := worker.New(r.tc, cfg.TaskQueue, worker.Options{
		// Note: workflow and activity concurrency are separately tunable in Temporal.
		MaxConcurrentActivityExecutionSize:     concurrency,
		MaxConcurrentWorkflowTaskExecutionSize: concurrency,
	})

	acts := &jobrun.Activities{
		Log:      r.log,
		DB:       r.db,
		Jobs:     r.jobRepo,
		Registry: r.registry,
		Notify:   r.notify,
	}

	w.RegisterWorkflowWithOptions(jobrun.Workflow, workflow.RegisterOptions{Name: jobrun.WorkflowName})
	w.RegisterActivityWithOptions(acts.Tick, activity.RegisterOptions{Name: jobrun.ActivityTick})
	return w, nil
}

func envTrue(key string, def bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	return strings.EqualFold(v, "true") || v == "1" || strings.EqualFold(v, "yes")
}

func durationSecondsFromEnv(key string, defSeconds int) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return time.Duration(defSeconds) * time.Second
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return time.Duration(defSeconds) * time.Second
	}
	if n < 0 {
		n = 0
	}
	return time.Duration(n) * time.Second
}

func durationMillisFromEnv(key string, defMillis int) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return time.Duration(defMillis) * time.Millisecond
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return time.Duration(defMillis) * time.Millisecond
	}
	if n < 0 {
		n = 0
	}
	return time.Duration(n) * time.Millisecond
}

func clampBackoff(base time.Duration, max time.Duration, attempt int) time.Duration {
	if base <= 0 {
		base = 250 * time.Millisecond
	}
	sleep := base
	for i := 1; i < attempt; i++ {
		sleep *= 2
		if max > 0 && sleep >= max {
			return max
		}
	}
	if max > 0 && sleep > max {
		return max
	}
	return sleep
}
