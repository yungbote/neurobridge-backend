package runtime

import (
	"fmt"
	"sync"
)

type Handler interface {
	Type() string
	Run(ctx *Context) error
}

type Registry struct {
	mu				sync.RWMutex
	handlers  map[string]Handler
}

func NewRegistry() *Registry {
	return &Registry{handlers: make(map[string]Handler)}
}

func (r *Registry) Register(h Handler) error {
	if h == nil {
		return fmt.Errorf("nil handler")
	}
	t := h.Type()
	if t == "" {
		return fmt.Errorf("handler Type() is empty")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.handlers[t]; exists {
		return fmt.Errorf("handler already registered for job_type=%s", t)
	}
	r.handlers[t] = h
	return nil
}

func (r *Registry) Get(jobType string) (Handler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.handlers[jobType]
	return h, ok
}










