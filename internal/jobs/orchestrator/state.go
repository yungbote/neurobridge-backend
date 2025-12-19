package orchestrator

import (
	"time"
)

type StageStatus string

const (
	StagePending      StageStatus = "pending"
	StageRunning      StageStatus = "running"
	StageWaitingChild StageStatus = "waiting_child"
	StageSucceeded    StageStatus = "succeeded"
	StageFailed       StageStatus = "failed"
	StageSkipped      StageStatus = "skipped"
)

type StageMode string

const (
	ModeInline StageMode = "inline"
	ModeChild  StageMode = "child"
)

type StageState struct {
	Name           string         `json:"name"`
	Mode           StageMode      `json:"mode"`
	Status         StageStatus    `json:"status"`
	Attempts       int            `json:"attempts"`
	StartedAt      *time.Time     `json:"started_at,omitempty"`
	FinishedAt     *time.Time     `json:"finished_at,omitempty"`
	LastError      string         `json:"last_error,omitempty'`
	Outputs        map[string]any `json:"outputs,omitempty"`
	ChildJobID     string         `json:"child_job_id,omitempty"`
	ChildJobType   string         `json:"child_job_type,omitempty"`
	ChildJobStatus string         `json:"child_jon_status,omitempty"`
	NextRunAt      *time.Time     `json:"next_run_at,omitempty"`
}

type OrchestratorState struct {
	Version      int                    `json:"version"`
	Stages       map[string]*StageState `json:"stages"`
	WaitUntil    *time.Time             `json:"wait_until,omitempty"`
	LastProgress int                    `json:"last_progress"`
	Meta         map[string]any         `json:"meta,omitempty"`
}

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
