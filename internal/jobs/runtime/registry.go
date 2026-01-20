package runtime

import (
	"fmt"
	"sync"
)

/*
The handler registry is the dispatch table for the job execution system.

Purpose:
	- Map a job_run.job_type *string* to a concrete handler implementation
	- Enforce a one-to-one relationship between job_type and handler
	- Provide a safe, concurrent lookup mechanism for workers

Idea:
	The registry is the *only* place where job_type -> code binding happens.
	Workers do not know about pipelines directly; they only ask the registry
	for a handler that claims responsibility for a given job_type.

Indirection is intentional:
	- It decouples job scheduling from business logic
	- It allows different executors (SQL worker, Temporal activity, tests, etc)
	  to reuse the same handler set
	- It makes misconfiguration (missing or duplicate handlers) explicit and fatal
*/

/*
Handler is the minimal contract required to execute a job.
Every pipeline must implement this interface.

Semantics:
	- Type() returns the job_type string this handler is responsible for.
	  This must exactly match job_run.job_type values stored in the db
	- Run(ctx) performs the job's work using runtime.Context as the only
	  mechanism to report progress, failure, or success.

IMPORTANT:
	- Handlers must be side-effect safe under retries
	- Handlers must assume they can be re-run after partial execution
*/
type Handler interface {
	Type() string
	Run(ctx *Context) error
}

/*
Registry is a concurrency-safe map of job_type -> handler.

Invariants:
	- At most one handler may be registered per job_type
	- Registration is expected to happen at process startup
	- Lookups may happen concurrently from many worker goroutines
*/
type Registry struct {
	mu       sync.RWMutex
	handlers map[string]Handler
}

/*
NewRegistry constructs an empty handler registry.

Typical usage:
	reg := runtime.NewRegistry()
	reg.Register(pipelineA)
	reg.Register(pipelineB)
	worker := worker.NewWorker(db, log, repo, reg, notify)
*/
func NewRegistry() *Registry {
	return &Registry{handlers: make(map[string]Handler)}
}

/*
Register adds a handler to the registry.

Safety checks:
	- Handler must not be nil
	- Handler.Type() must return a non-empty string
	- No other handler may already be registered for the same job_type

Why duplicate registration is forbidden:
	- job_type ambiguity would make execution non-deterministic
	- It is almost always a wiring/configuration error
	- Failing fast at startup is far better than silently picking one
*/
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

/*
Get retrieves the handler responsible for a given job_type.

Returns:
	- (handler, true) if a handler is registered
	- (nil, false) if no handler exists for job_type

Concurrency:
	- Uses a read lock so lookups can scale across many workers

Worker behavior on miss:
	- The worker treats a missing handler as a fatal job error,
	  because it indicates a deployment or wiring issue, not a retryable
	  condition.
*/
func (r *Registry) Get(jobType string) (Handler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.handlers[jobType]
	return h, ok
}
