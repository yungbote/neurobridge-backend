package handlers

import (
	"testing"

	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

func newTestLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.New("development")
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}
	t.Cleanup(log.Sync)
	return log
}

func TestNewPathHandlerWithDeps(t *testing.T) {
	log := newTestLogger(t)
	h := NewPathHandlerWithDeps(PathHandlerDeps{Log: log})
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
}

func TestNewRuntimeStateHandlerWithDeps(t *testing.T) {
	h := NewRuntimeStateHandlerWithDeps(RuntimeStateHandlerDeps{})
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
}

func TestNewMaterialHandlerWithDeps(t *testing.T) {
	log := newTestLogger(t)
	h := NewMaterialHandlerWithDeps(MaterialHandlerDeps{Log: log})
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
}

func TestNewLibraryHandlerWithDeps(t *testing.T) {
	log := newTestLogger(t)
	h := NewLibraryHandlerWithDeps(LibraryHandlerDeps{Log: log})
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
}

func TestNewActivityHandlerWithDeps(t *testing.T) {
	log := newTestLogger(t)
	h := NewActivityHandlerWithDeps(ActivityHandlerDeps{Log: log})
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
}

func TestNewEventHandlerWithDeps(t *testing.T) {
	h := NewEventHandlerWithDeps(EventHandlerDeps{})
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
}
