package testutil

import (
	"context"
	"sync"

	"github.com/yungbote/neurobridge-backend/internal/data/aggregates"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

// InjectedTxRunner is a test helper for aggregate integration tests.
// It supports rollback/failure injection without touching a real DB.
type InjectedTxRunner struct {
	mu sync.Mutex

	FailBegin      error
	FailBeforeBody error
	FailCommit     error

	BeginCalls    int
	CommitCalls   int
	RollbackCalls int
}

var _ aggregates.TxRunner = (*InjectedTxRunner)(nil)

func (r *InjectedTxRunner) InTx(ctx context.Context, fn func(dbc dbctx.Context) error) error {
	r.mu.Lock()
	r.BeginCalls++
	failBegin := r.FailBegin
	failBeforeBody := r.FailBeforeBody
	failCommit := r.FailCommit
	r.mu.Unlock()

	if failBegin != nil {
		return failBegin
	}
	if failBeforeBody != nil {
		r.mu.Lock()
		r.RollbackCalls++
		r.mu.Unlock()
		return failBeforeBody
	}
	if fn == nil {
		r.mu.Lock()
		r.CommitCalls++
		r.mu.Unlock()
		return nil
	}
	if err := fn(dbctx.Context{Ctx: ctx}); err != nil {
		r.mu.Lock()
		r.RollbackCalls++
		r.mu.Unlock()
		return err
	}
	if failCommit != nil {
		r.mu.Lock()
		r.RollbackCalls++
		r.mu.Unlock()
		return failCommit
	}
	r.mu.Lock()
	r.CommitCalls++
	r.mu.Unlock()
	return nil
}
