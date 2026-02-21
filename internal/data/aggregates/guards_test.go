package aggregates

import "testing"

func TestRequireStatusAllowed(t *testing.T) {
	if err := RequireStatusAllowed("running", "running", "queued"); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if err := RequireStatusAllowed("failed", "running", "queued"); err == nil {
		t.Fatalf("expected conflict error")
	}
}

func TestRequireVersionMatch(t *testing.T) {
	if err := RequireVersionMatch(3, 3); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if err := RequireVersionMatch(2, 3); err == nil {
		t.Fatalf("expected conflict error")
	}
}

func TestRequireCASSuccess(t *testing.T) {
	if err := RequireCASSuccess(true, "ok"); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if err := RequireCASSuccess(false, "stale"); err == nil {
		t.Fatalf("expected conflict error")
	}
}
