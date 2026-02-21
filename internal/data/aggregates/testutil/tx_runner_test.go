package testutil

import (
	"context"
	"errors"
	"testing"

	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

func TestInjectedTxRunner_CommitsOnSuccess(t *testing.T) {
	r := &InjectedTxRunner{}
	called := false
	err := r.InTx(context.Background(), func(_ dbctx.Context) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !called {
		t.Fatalf("expected callback to run")
	}
	if r.BeginCalls != 1 || r.CommitCalls != 1 || r.RollbackCalls != 0 {
		t.Fatalf("unexpected counters begin=%d commit=%d rollback=%d", r.BeginCalls, r.CommitCalls, r.RollbackCalls)
	}
}

func TestInjectedTxRunner_RollbackOnBodyError(t *testing.T) {
	r := &InjectedTxRunner{}
	bodyErr := errors.New("boom")
	err := r.InTx(context.Background(), func(_ dbctx.Context) error {
		return bodyErr
	})
	if !errors.Is(err, bodyErr) {
		t.Fatalf("expected body err, got %v", err)
	}
	if r.BeginCalls != 1 || r.CommitCalls != 0 || r.RollbackCalls != 1 {
		t.Fatalf("unexpected counters begin=%d commit=%d rollback=%d", r.BeginCalls, r.CommitCalls, r.RollbackCalls)
	}
}

func TestInjectedTxRunner_FailCommitTriggersRollback(t *testing.T) {
	commitErr := errors.New("commit failed")
	r := &InjectedTxRunner{FailCommit: commitErr}
	err := r.InTx(context.Background(), func(_ dbctx.Context) error {
		return nil
	})
	if !errors.Is(err, commitErr) {
		t.Fatalf("expected commit err, got %v", err)
	}
	if r.BeginCalls != 1 || r.CommitCalls != 0 || r.RollbackCalls != 1 {
		t.Fatalf("unexpected counters begin=%d commit=%d rollback=%d", r.BeginCalls, r.CommitCalls, r.RollbackCalls)
	}
}
