package learning_build

import (
	"encoding/json"
	"time"
)

const (
	stateVersion = 1

	stageStatusPending      = "pending"
	stageStatusWaitingChild = "waiting_child"
	stageStatusSucceeded    = "succeeded"
	stageStatusFailed       = "failed"
)

type state struct {
	Version       int                    `json:"version"`
	Mode          string                 `json:"mode,omitempty"`
	MaterialSetID string                 `json:"material_set_id,omitempty"`
	SagaID        string                 `json:"saga_id,omitempty"`
	PathID        string                 `json:"path_id,omitempty"`
	Stages        map[string]*stageState `json:"stages,omitempty"`
	WaitUntil     *time.Time             `json:"wait_until,omitempty"`
	LastProgress  int                    `json:"last_progress,omitempty"`
}

type stageState struct {
	Name           string     `json:"name"`
	Status         string     `json:"status"`
	ChildJobID     string     `json:"child_job_id,omitempty"`
	ChildJobStatus string     `json:"child_job_status,omitempty"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
	LastError      string     `json:"last_error,omitempty"`
	ChildResult    any        `json:"child_result,omitempty"`
}

func newState() *state {
	return &state{
		Version:      stateVersion,
		Stages:       map[string]*stageState{},
		LastProgress: 0,
	}
}

func loadState(raw []byte) *state {
	st := newState()
	if len(raw) == 0 || string(raw) == "null" {
		return st
	}
	var tmp state
	if err := json.Unmarshal(raw, &tmp); err != nil {
		return st
	}
	if tmp.Version <= 0 {
		tmp.Version = stateVersion
	}
	if tmp.Stages == nil {
		tmp.Stages = map[string]*stageState{}
	}
	if tmp.LastProgress < 0 {
		tmp.LastProgress = 0
	}
	return &tmp
}

func (s *state) ensureStage(name string) *stageState {
	if s.Stages == nil {
		s.Stages = map[string]*stageState{}
	}
	ss := s.Stages[name]
	if ss == nil {
		ss = &stageState{Name: name, Status: stageStatusPending}
		s.Stages[name] = ss
	}
	if ss.Status == "" {
		ss.Status = stageStatusPending
	}
	return ss
}

func (s *state) setProgress(pct int) int {
	if pct < s.LastProgress {
		return s.LastProgress
	}
	s.LastProgress = pct
	return pct
}
