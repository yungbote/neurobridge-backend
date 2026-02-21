package testutil

import (
	"sync"
	"time"

	"github.com/yungbote/neurobridge-backend/internal/data/aggregates"
)

// HooksRecorder captures aggregate hook signals in tests.
type HooksRecorder struct {
	mu sync.Mutex

	Operations []OperationEvent
	Conflicts  []string
	Retries    []string
}

type OperationEvent struct {
	Name     string
	Status   string
	Duration time.Duration
}

var _ aggregates.Hooks = (*HooksRecorder)(nil)

func (h *HooksRecorder) ObserveOperation(name, status string, dur time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.Operations = append(h.Operations, OperationEvent{
		Name:     name,
		Status:   status,
		Duration: dur,
	})
}

func (h *HooksRecorder) IncConflict(name string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.Conflicts = append(h.Conflicts, name)
}

func (h *HooksRecorder) IncRetry(name string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.Retries = append(h.Retries, name)
}
