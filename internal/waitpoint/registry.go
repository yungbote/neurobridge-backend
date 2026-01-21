package waitpoint

import (
	"fmt"
	"sync"
)

type Registry struct {
	mu		sync.RWMutex
	byKind	map[string]Config
}

func NewRegistry() *Registry {
	return &Registry{
		byKind: map[string]Config{},
	}
}

func (r *Registry) Register(cfg Config) error {
	if r == nil {
		return fmt.Errorf("nil registry")
	}
	if cfg.Kind == "" {
		return fmt.Errorf("missing cfg.Kind")
	}
	if cfg.BuildClassifierPrompt == nil {
		return fmt.Errorf("cfg.BuildClassifierPrompt is nil (kind=%s)", cfg.Kind)
	}
	if cfg.Reduce == nil {
		return fmt.Errorf("cfg.Reduce is nil (kind=%s)", cfg.Kind)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byKind[cfg.Kind]; ok {
		return fmt.Errorf("waitpoint config already registered for kind=%s", cfg.Kind)
	}
	r.byKind[cfg.Kind] = cfg
	return nil
}

func (r *Registry) Get(kind string) (Config, bool) {
	if r == nil {
		return Config{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	cfg, ok := r.byKind[kind]
	return cfg, ok
}










