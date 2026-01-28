package orchestrator

import (
	"time"
)

/*
This file defines the *persisted state model* for a resumable orchestration workflow.
Everything in this file is **data**, not behavior.

The orchestrator engine (dag.go) is a pure state machine that:
	- loads this state from durable storage,
	- mutates it deterministically,
	- persists it back,
	- and resumes later from the same snapshot.

Principle:
	The workflow must be restartable at any point with no in-memory assumptions.
	All information required to continue execution lives in these structs.
*/

/*
StageStatus represents the lifecycle state of a single stage within a workflow.
These values are persisted and must be stable across deployments.

Semantics:
  - pending: Stage has not started yet
  - running: Stage is currently executing inline code
  - waiting_child: Stage has spawned a child job and is waiting for it
  - succeeded: Stage completed successfully
  - failed: Stage failed and will not be retried (unless reset by policy)
  - skipped: Stage was intentionally skipped (e.g., conditional execution)
*/
type StageStatus string

const (
	StagePending      StageStatus = "pending"
	StageRunning      StageStatus = "running"
	StageWaitingChild StageStatus = "waiting_child"
	StageSucceeded    StageStatus = "succeeded"
	StageFailed       StageStatus = "failed"
	StageSkipped      StageStatus = "skipped"
)

/*
StageMode controls *how* a stage is executed.

Semantics:
  - inline:
    The stage runs synchronously inside the orchestrator process.
    The orchestrator directly calls the stage's Run function.
  - child:
    The stage is executed as a separate job_run.
    The orchestrator enqueues a child job and polls its status.

Most stages are ModeChild to:
  - allow parallelism,
  - avoid blocking workers,
  - and support pause/resume via DB state.
*/
type StageMode string

const (
	ModeInline StageMode = "inline"
	ModeChild  StageMode = "child"
)

/*
StageState is the *entire durable execution record* for a single stage.

This struct is written to storage and later reloaded verbatim.
Nothing about a stage's progress or child job linkage is kept in memory.

Fields are intentionally redundant and explicit to support:
  - crash recovery,
  - retries with backoff,
  - UI progress reporting,
  - parent/child job supervision.
*/
type StageState struct {
	Name           string         `json:"name"`                       // logical name of the stage (must be unique within a workflow)
	Deps           []string       `json:"deps,omitempty"`             // Static dependency list captured from the stage definition for instrumentation/debugging
	Mode           StageMode      `json:"mode"`                       // Execution mode: inline or child
	Status         StageStatus    `json:"status"`                     // Current lifecycle status of the stage
	Attempts       int            `json:"attempts"`                   // Number of execution attempts so faqr (used for retry policies)
	StartedAt      *time.Time     `json:"started_at,omitempty"`       // Timestamp when the stage first began execution
	FinishedAt     *time.Time     `json:"finished_at,omitempty"`      // Timestamp when the stage reached a terminal state
	LastError      string         `json:"last_error,omitempty"`       // Last error message observed for this stage (terminal or retryable)
	Outputs        map[string]any `json:"outputs,omitempty"`          // Arbitrary key/value outputs produced by the stage. Used primarily for inline stages or metadata aggregation
	ChildResult    any            `json:"child_result,omitempty"`     // Parsed result payload from a completed child job, if applicable
	ChildJobID     string         `json:"child_job_id,omitempty"`     // Identifier of the child job_run spawned for this stage (ModeChild only)
	ChildJobType   string         `json:"child_job_type,omitempty"`   // Job type of the child job (useful for debugging/UI)
	ChildJobStatus string         `json:"child_job_status,omitempty"` // Last known status of the child job
	ChildProgress  int            `json:"child_progress,omitempty"`   // Last reported progress percentage of the child job
	ChildMessage   string         `json:"child_message,omitempty"`    // Last human-readable message reported by the child job
	NextRunAt      *time.Time     `json:"next_run_at,omitempty"`      // Earliest time this stage is allowed to run again (retry backoff). When set, the orchestrator will not attempt execution before this time
}

/*
OrchestratorState is the *root snapshot* of a workflow execution.

This object is:
  - serialized into the parent job_run.result field,
  - updated incrementally as stages advance,
  - and reloaded on every resume.

It is the source of truth for orchestration progress.
*/
type OrchestratorState struct {
	Version      int                    `json:"version"`              // Version of the state schema (for future migrations)
	Stages       map[string]*StageState `json:"stages"`               // All stages in the workflow, keyed by stage name
	WaitUntil    *time.Time             `json:"wait_until,omitempty"` // Global gate to prevent tight polling. If set, the orchestrator will not attempt progress before this time
	LastProgress int                    `json:"last_progress"`        // Monotonic progress value (0-99) used for UI stability. Prevents progress from moving backwards across resumes
	Meta         map[string]any         `json:"meta,omitempty"`       // Freeform workflow metadata. Used to store identifiers like 'material_set_id', 'saga_id', 'path_id', etc
}

/*
ensure initializes default values for the orchestrator state.

This is called defensively whenever state is loaded or mutated to guarantee:
  - Version is non-zero,
  - Stages map is allocated,
  - Meta map is allocated.

This allows older or partially-written states to be safely resumed.
*/
func (s *OrchestratorState) ensure() {
	if s.Version <= 0 {
		s.Version = 1
	}
	if s.Stages == nil {
		s.Stages = map[string]*StageState{}
	}
	if s.Meta == nil {
		s.Meta = map[string]any{}
	}
}

/*
EnsureStage returns the StageState for a given stage name, creating it if needed.

Behavior:
  - Idempotent: safe to call repeatedly on resume
  - Initializes new stages as:
  - Status = pending
  - Mode = provided mode
  - Outputs = empty map

Invariants:
  - A stage's Mode is only set once (on first creation)
  - Outputs map is always non-nil after this call

This method is the *only* sanctioned way for the orchestrator engine to materialize stage state.
*/
func (s *OrchestratorState) EnsureStage(name string, mode StageMode) *StageState {
	s.ensure()
	ss := s.Stages[name]
	if ss == nil {
		ss = &StageState{
			Name:    name,
			Mode:    mode,
			Status:  StagePending,
			Outputs: map[string]any{},
		}
		s.Stages[name] = ss
	}
	if ss.Outputs == nil {
		ss.Outputs = map[string]any{}
	}
	if ss.Mode == "" {
		ss.Mode = mode
	}
	return ss
}
