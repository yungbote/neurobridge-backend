package jobrun

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	jobrt "github.com/yungbote/neurobridge-backend/internal/jobs/runtime"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"

	"go.temporal.io/sdk/activity"
)

type Activities struct {
	Log      *logger.Logger
	DB       *gorm.DB
	Jobs     repos.JobRunRepo
	Registry *jobrt.Registry
	Notify   services.JobNotifier
}

func (a *Activities) Tick(ctx context.Context, jobID string) (TickResult, error) {
	res := TickResult{JobID: strings.TrimSpace(jobID)}
	if a == nil || a.DB == nil || a.Jobs == nil || a.Registry == nil {
		return res, fmt.Errorf("jobrun: activity not configured")
	}

	parsedJobID, err := uuid.Parse(res.JobID)
	if err != nil || parsedJobID == uuid.Nil {
		return res, fmt.Errorf("jobrun: invalid job_id")
	}

	job, err := a.loadJob(ctx, parsedJobID)
	if err != nil {
		return res, err
	}
	if job == nil {
		return res, fmt.Errorf("jobrun: job not found")
	}

	status := strings.ToLower(strings.TrimSpace(job.Status))
	if status == "succeeded" || status == "failed" || status == "canceled" || status == "waiting_user" {
		if a.Notify != nil && job.OwnerUserID != uuid.Nil {
			switch status {
			case "succeeded":
				a.Notify.JobDone(job.OwnerUserID, job)
			case "failed":
				a.Notify.JobFailed(job.OwnerUserID, job, job.Stage, strings.TrimSpace(job.Error))
			case "canceled":
				a.Notify.JobCanceled(job.OwnerUserID, job)
			case "waiting_user":
				a.Notify.JobProgress(job.OwnerUserID, job, job.Stage, job.Progress, job.Message)
			}
		}
		res.Status = job.Status
		res.Stage = job.Stage
		res.Progress = job.Progress
		res.Message = job.Message
		res.WaitUntil = extractWaitUntil(job.Result)
		return res, nil
	}

	stopHB := a.startHeartbeat(ctx, parsedJobID)
	defer stopHB()

	// Mark running (best-effort; if canceled concurrently, do nothing).
	now := time.Now().UTC()
	_ = a.DB.WithContext(ctx).
		Model(&types.JobRun{}).
		Where("id = ? AND status <> ?", parsedJobID, "canceled").
		Updates(map[string]any{
			"status":       "running",
			"attempts":     gorm.Expr("attempts + 1"),
			"locked_at":    now,
			"heartbeat_at": now,
			"updated_at":   now,
		}).Error

	job.Status = "running"
	job.LockedAt = &now
	job.HeartbeatAt = &now
	job.UpdatedAt = now

	handlerReturnedNil := false
	h, ok := a.Registry.Get(job.JobType)
	jc := jobrt.NewContext(ctx, a.DB, job, a.Jobs, a.Notify)
	if !ok {
		jc.Fail("dispatch", fmt.Errorf("no handler registered for job_type=%s", job.JobType))
	} else {
		func() {
			defer func() {
				if r := recover(); r != nil {
					if a.Log != nil {
						a.Log.Error("Job handler panic", "job_id", parsedJobID, "job_type", job.JobType, "panic", r)
					}
					jc.Fail("panic", fmt.Errorf("panic: unexpected error"))
				}
			}()
			if runErr := h.Run(jc); runErr != nil {
				jc.Fail("run", runErr)
				return
			}
			handlerReturnedNil = true
		}()
	}

	updated, err := a.loadJob(ctx, parsedJobID)
	if err != nil {
		return res, err
	}
	if updated == nil {
		return res, fmt.Errorf("jobrun: job not found after tick")
	}

	// Safety net: if a handler returns nil but never marks the job terminal (succeed/fail/cancel)
	// and also didn't yield (queued/waiting_user), then the job would otherwise stick in "running"
	// forever and block upstream DAGs. Treat that as success, preserving any existing result.
	if handlerReturnedNil && strings.EqualFold(strings.TrimSpace(updated.Status), "running") {
		if a.Log != nil {
			a.Log.Warn("Job handler returned nil without terminal status; marking succeeded", "job_id", parsedJobID, "job_type", updated.JobType, "stage", updated.Stage)
		}
		finalStage := "done"
		if s := strings.TrimSpace(updated.Stage); s != "" && !strings.EqualFold(s, "queued") && !strings.EqualFold(s, "running") {
			finalStage = s
		}
		var finalResult any
		if len(updated.Result) > 0 && strings.TrimSpace(string(updated.Result)) != "" && strings.TrimSpace(string(updated.Result)) != "null" {
			finalResult = json.RawMessage(updated.Result)
		}
		jc.Succeed(finalStage, finalResult)

		// Reload once so the TickResult reflects the terminal state.
		if r2, rerr := a.loadJob(ctx, parsedJobID); rerr == nil && r2 != nil {
			updated = r2
		}
	}

	res.Status = updated.Status
	res.Stage = updated.Stage
	res.Progress = updated.Progress
	res.Message = updated.Message
	res.WaitUntil = extractWaitUntil(updated.Result)
	return res, nil
}

func (a *Activities) loadJob(ctx context.Context, jobID uuid.UUID) (*types.JobRun, error) {
	rows, err := a.Jobs.GetByIDs(dbctx.Context{Ctx: ctx, Tx: a.DB}, []uuid.UUID{jobID})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 || rows[0] == nil {
		return nil, nil
	}
	return rows[0], nil
}

func (a *Activities) startHeartbeat(ctx context.Context, jobID uuid.UUID) func() {
	done := make(chan struct{})
	go func() {
		temporalHB := time.NewTicker(10 * time.Second)
		defer temporalHB.Stop()

		dbHB := time.NewTicker(30 * time.Second)
		defer dbHB.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-temporalHB.C:
				activity.RecordHeartbeat(ctx)
			case <-dbHB.C:
				if a == nil || a.DB == nil || a.Jobs == nil || jobID == uuid.Nil {
					continue
				}
				_ = a.Jobs.Heartbeat(dbctx.Context{Ctx: ctx, Tx: a.DB}, jobID)
			}
		}
	}()
	return func() { close(done) }
}

func extractWaitUntil(raw []byte) *time.Time {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "" || strings.TrimSpace(string(raw)) == "null" {
		return nil
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil
	}
	v, ok := obj["wait_until"]
	if !ok || v == nil {
		return nil
	}
	switch x := v.(type) {
	case string:
		ts, err := time.Parse(time.RFC3339Nano, x)
		if err != nil {
			ts, err = time.Parse(time.RFC3339, x)
			if err != nil {
				return nil
			}
		}
		return &ts
	default:
		return nil
	}
}
