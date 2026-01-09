package jobrun

import (
	"fmt"
	"strings"
	"time"

	"go.temporal.io/sdk/workflow"
)

func Workflow(ctx workflow.Context) error {
	jobID := strings.TrimSpace(workflow.GetInfo(ctx).WorkflowExecution.ID)
	if strings.TrimSpace(jobID) == "" {
		return fmt.Errorf("jobrun: missing job_id")
	}

	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 24 * time.Hour,
		HeartbeatTimeout:    30 * time.Second,
		RetryPolicy:         nil, // job retries are handled at the workflow level
	})

	resumeCh := workflow.GetSignalChannel(ctx, SignalResume)

	for {
		var out TickResult
		if err := workflow.ExecuteActivity(ctx, ActivityTick, jobID).Get(ctx, &out); err != nil {
			return err
		}

		status := strings.ToLower(strings.TrimSpace(out.Status))
		switch status {
		case "succeeded", "canceled":
			return nil
		case "failed":
			return fmt.Errorf("job failed (stage=%s)", strings.TrimSpace(out.Stage))
		case "waiting_user":
			waitForResumeOrPoll(ctx, resumeCh, 30*time.Second)
			continue
		default:
			if d := nextWait(ctx, out.WaitUntil, 2*time.Second); d > 0 {
				if err := workflow.Sleep(ctx, d); err != nil {
					return err
				}
			}
			continue
		}
	}
}

func waitForResumeOrPoll(ctx workflow.Context, ch workflow.ReceiveChannel, maxWait time.Duration) {
	timer := workflow.NewTimer(ctx, maxWait)
	sel := workflow.NewSelector(ctx)
	sel.AddReceive(ch, func(c workflow.ReceiveChannel, more bool) {
		var v any
		c.Receive(ctx, &v)
	})
	sel.AddFuture(timer, func(f workflow.Future) {})
	sel.Select(ctx)
}

func nextWait(ctx workflow.Context, waitUntil *time.Time, def time.Duration) time.Duration {
	if waitUntil == nil || waitUntil.IsZero() {
		return def
	}
	now := workflow.Now(ctx)
	if waitUntil.Before(now) {
		return def
	}
	d := waitUntil.Sub(now)
	if d <= 0 {
		return def
	}
	if d > 15*time.Minute {
		return 15 * time.Minute
	}
	return d
}
