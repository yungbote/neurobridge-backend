package aggregates

import (
	"strings"
	"time"

	"github.com/yungbote/neurobridge-backend/internal/observability"
)

// Hooks captures aggregate-level observability events.
type Hooks interface {
	ObserveOperation(name, status string, dur time.Duration)
	IncConflict(name string)
	IncRetry(name string)
}

type noopHooks struct{}

func (noopHooks) ObserveOperation(string, string, time.Duration) {}
func (noopHooks) IncConflict(string)                             {}
func (noopHooks) IncRetry(string)                                {}

type observabilityHooks struct {
	metrics *observability.Metrics
}

// NewObservabilityHooks creates aggregate hooks backed by observability metrics.
func NewObservabilityHooks(metrics *observability.Metrics) Hooks {
	if metrics == nil {
		return noopHooks{}
	}
	return &observabilityHooks{metrics: metrics}
}

func (h *observabilityHooks) ObserveOperation(name, status string, dur time.Duration) {
	if h == nil || h.metrics == nil {
		return
	}
	h.metrics.ObserveAggregateOperation(strings.TrimSpace(name), strings.TrimSpace(status), dur)
}

func (h *observabilityHooks) IncConflict(name string) {
	if h == nil || h.metrics == nil {
		return
	}
	h.metrics.IncAggregateConflict(strings.TrimSpace(name))
}

func (h *observabilityHooks) IncRetry(name string) {
	if h == nil || h.metrics == nil {
		return
	}
	h.metrics.IncAggregateRetry(strings.TrimSpace(name))
}
