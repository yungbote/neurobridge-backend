package jobrun

import "time"

const (
	WorkflowName = "job_run"
	ActivityTick = "job_run_tick"
	SignalResume = "job_resume"
)

type TickResult struct {
	JobID     string     `json:"job_id"`
	Status    string     `json:"status"`
	Stage     string     `json:"stage,omitempty"`
	Progress  int        `json:"progress,omitempty"`
	Message   string     `json:"message,omitempty"`
	WaitUntil *time.Time `json:"wait_until,omitempty"`
}
