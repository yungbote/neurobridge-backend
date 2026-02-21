package testutil

import (
	"testing"
	"time"
)

func TestHooksRecorder_CapturesSignals(t *testing.T) {
	h := &HooksRecorder{}
	h.ObserveOperation("agg.op", "success", 10*time.Millisecond)
	h.IncConflict("agg.op")
	h.IncRetry("agg.op")

	if len(h.Operations) != 1 {
		t.Fatalf("expected 1 op event, got %d", len(h.Operations))
	}
	if h.Operations[0].Name != "agg.op" || h.Operations[0].Status != "success" {
		t.Fatalf("unexpected op event: %+v", h.Operations[0])
	}
	if len(h.Conflicts) != 1 || h.Conflicts[0] != "agg.op" {
		t.Fatalf("unexpected conflicts: %+v", h.Conflicts)
	}
	if len(h.Retries) != 1 || h.Retries[0] != "agg.op" {
		t.Fatalf("unexpected retries: %+v", h.Retries)
	}
}
