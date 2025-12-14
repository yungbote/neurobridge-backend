package services

import (
	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/sse"
	"github.com/yungbote/neurobridge-backend/internal/types"
)

type JobNotifier interface {
	JobCreated(userID uuid.UUID, job *types.JobRun)
	JobProgress(userID uuid.UUID, job *types.JobRun, stage string, progress int, message string)
	JobFailed(userID uuid.UUID, job *types.JobRun, stage string, errorMessage string)
	JobDone(userID uuid.UUID, job *types.JobRun)
}

type jobNotifier struct {
	hub *sse.SSEHub
}

func NewJobNotifier(hub *sse.SSEHub) JobNotifier {
	return &jobNotifier{hub: hub}
}

func (n *jobNotifier) JobCreated(userID uuid.UUID, job *types.JobRun) {
	n.hub.Broadcast(sse.SSEMessage{
		Channel: userID.String(),
		Event:   sse.SSEEventJobCreated,
		Data:    map[string]any{"job": job},
	})
}

func (n *jobNotifier) JobProgress(userID uuid.UUID, job *types.JobRun, stage string, progress int, message string) {
	n.hub.Broadcast(sse.SSEMessage{
		Channel: userID.String(),
		Event:   sse.SSEEventJobProgress,
		Data: map[string]any{
			"job_id":   job.ID,
			"job_type": job.JobType,
			"stage":    stage,
			"progress": progress,
			"message":  message,
			"job":      job,
		},
	})
}

func (n *jobNotifier) JobFailed(userID uuid.UUID, job *types.JobRun, stage string, errorMessage string) {
	n.hub.Broadcast(sse.SSEMessage{
		Channel: userID.String(),
		Event:   sse.SSEEventJobFailed,
		Data: map[string]any{
			"job_id":   job.ID,
			"job_type": job.JobType,
			"stage":    stage,
			"error":    errorMessage,
			"job":      job,
		},
	})
}

func (n *jobNotifier) JobDone(userID uuid.UUID, job *types.JobRun) {
	n.hub.Broadcast(sse.SSEMessage{
		Channel: userID.String(),
		Event:   sse.SSEEventJobDone,
		Data: map[string]any{
			"job_id":   job.ID,
			"job_type": job.JobType,
			"job":      job,
		},
	})
}










