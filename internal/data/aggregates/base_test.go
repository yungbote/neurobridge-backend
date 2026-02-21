package aggregates

import (
	"context"
	"testing"
	"time"

	domainagg "github.com/yungbote/neurobridge-backend/internal/domain/aggregates"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

func TestExecuteWriteObservesSuccessStatus(t *testing.T) {
	hooks := &spyHooks{}
	runner := spyTxRunner{}

	err := executeWrite(context.Background(), BaseDeps{
		Runner: runner,
		Hooks:  hooks,
	}, "aggregate.test.success", func(_ dbctx.Context) error { return nil })
	if err != nil {
		t.Fatalf("executeWrite success: %v", err)
	}
	if len(hooks.Operations) != 1 {
		t.Fatalf("operations count: want=1 got=%d", len(hooks.Operations))
	}
	if hooks.Operations[0].Status != "success" {
		t.Fatalf("operation status: want=success got=%s", hooks.Operations[0].Status)
	}
}

func TestExecuteWriteObservesInvariantViolationStatus(t *testing.T) {
	hooks := &spyHooks{}
	runner := spyTxRunner{}

	err := executeWrite(context.Background(), BaseDeps{
		Runner: runner,
		Hooks:  hooks,
	}, "aggregate.test.invariant", func(_ dbctx.Context) error {
		return InvariantError("invariant broken")
	})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !domainagg.IsCode(err, domainagg.CodeInvariantViolation) {
		t.Fatalf("expected invariant violation code, got=%v", err)
	}
	if len(hooks.Operations) != 1 {
		t.Fatalf("operations count: want=1 got=%d", len(hooks.Operations))
	}
	if hooks.Operations[0].Status != string(domainagg.CodeInvariantViolation) {
		t.Fatalf("operation status: want=%s got=%s", domainagg.CodeInvariantViolation, hooks.Operations[0].Status)
	}
}

func TestExecuteWriteTracksConflictAndRetryCounters(t *testing.T) {
	t.Run("conflict", func(t *testing.T) {
		hooks := &spyHooks{}
		runner := spyTxRunner{}
		err := executeWrite(context.Background(), BaseDeps{
			Runner: runner,
			Hooks:  hooks,
		}, "aggregate.test.conflict", func(_ dbctx.Context) error {
			return ConflictError("stale version")
		})
		if err == nil {
			t.Fatalf("expected error")
		}
		if !domainagg.IsCode(err, domainagg.CodeConflict) {
			t.Fatalf("expected conflict code, got=%v", err)
		}
		if len(hooks.Conflicts) != 1 || hooks.Conflicts[0] != "aggregate.test.conflict" {
			t.Fatalf("conflict hooks: %+v", hooks.Conflicts)
		}
		if len(hooks.Retries) != 0 {
			t.Fatalf("retry hooks should be empty, got=%+v", hooks.Retries)
		}
		if len(hooks.Operations) != 1 || hooks.Operations[0].Status != string(domainagg.CodeConflict) {
			t.Fatalf("unexpected op status: %+v", hooks.Operations)
		}
	})

	t.Run("retryable", func(t *testing.T) {
		hooks := &spyHooks{}
		runner := spyTxRunner{}
		err := executeWrite(context.Background(), BaseDeps{
			Runner: runner,
			Hooks:  hooks,
		}, "aggregate.test.retry", func(_ dbctx.Context) error {
			return RetryableError("temporary lock timeout")
		})
		if err == nil {
			t.Fatalf("expected error")
		}
		if !domainagg.IsCode(err, domainagg.CodeRetryable) {
			t.Fatalf("expected retryable code, got=%v", err)
		}
		if len(hooks.Retries) != 1 || hooks.Retries[0] != "aggregate.test.retry" {
			t.Fatalf("retry hooks: %+v", hooks.Retries)
		}
		if len(hooks.Conflicts) != 0 {
			t.Fatalf("conflict hooks should be empty, got=%+v", hooks.Conflicts)
		}
		if len(hooks.Operations) != 1 || hooks.Operations[0].Status != string(domainagg.CodeRetryable) {
			t.Fatalf("unexpected op status: %+v", hooks.Operations)
		}
	})
}

// compile-time guard to catch accidental status format regressions.
func TestAggregateErrorStatus(t *testing.T) {
	if got := aggregateErrorStatus(nil); got != "success" {
		t.Fatalf("nil status: want=success got=%s", got)
	}
	if got := aggregateErrorStatus(InvariantError("x")); got != string(domainagg.CodeInvariantViolation) {
		t.Fatalf("invariant status: got=%s", got)
	}
	if got := aggregateErrorStatus(ConflictError("x")); got != string(domainagg.CodeConflict) {
		t.Fatalf("conflict status: got=%s", got)
	}
	if got := aggregateErrorStatus(RetryableError("x")); got != string(domainagg.CodeRetryable) {
		t.Fatalf("retry status: got=%s", got)
	}
	if got := aggregateErrorStatus(context.DeadlineExceeded); got != string(domainagg.CodeRetryable) {
		t.Fatalf("deadline status: got=%s", got)
	}
}

type spyTxRunner struct{}

func (spyTxRunner) InTx(ctx context.Context, fn func(dbc dbctx.Context) error) error {
	if fn == nil {
		return nil
	}
	return fn(dbctx.Context{Ctx: ctx})
}

type spyHooks struct {
	Operations []spyOperation
	Conflicts  []string
	Retries    []string
}

type spyOperation struct {
	Name   string
	Status string
}

func (h *spyHooks) ObserveOperation(name, status string, _ time.Duration) {
	h.Operations = append(h.Operations, spyOperation{Name: name, Status: status})
}

func (h *spyHooks) IncConflict(name string) {
	h.Conflicts = append(h.Conflicts, name)
}

func (h *spyHooks) IncRetry(name string) {
	h.Retries = append(h.Retries, name)
}
