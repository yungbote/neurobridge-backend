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



type CourseNotifier interface {
	// Domain event: “here is the current course snapshot”
	CourseCreated(userID uuid.UUID, course *types.Course, job *types.JobRun)

	// Domain events: “course generation is progressing”
	CourseGenerationProgress(userID uuid.UUID, course *types.Course, job *types.JobRun, stage string, progress int, message string)
	CourseGenerationFailed(userID uuid.UUID, course *types.Course, job *types.JobRun, stage string, errorMessage string)
	CourseGenerationDone(userID uuid.UUID, course *types.Course, job *types.JobRun)
}

type courseNotifier struct {
	hub *sse.SSEHub
}

func NewCourseNotifier(hub *sse.SSEHub) CourseNotifier {
	return &courseNotifier{hub: hub}
}

func (n *courseNotifier) CourseCreated(userID uuid.UUID, course *types.Course, job *types.JobRun) {
	n.hub.Broadcast(sse.SSEMessage{
		Channel: userID.String(),
		Event:   sse.SSEEventUserCourseCreated,
		Data: map[string]any{
			"course": course,
			"job":    job,
		},
	})
}

func (n *courseNotifier) CourseGenerationProgress(userID uuid.UUID, course *types.Course, job *types.JobRun, stage string, progress int, message string) {
	n.hub.Broadcast(sse.SSEMessage{
		Channel: userID.String(),
		Event:   sse.CourseGenerationProgress,
		Data: map[string]any{
			"course_id": safeCourseID(course),
			"course":    course,

			"job_id":   safeJobID(job),
			"job_type": safeJobType(job),
			"job":      job,

			"stage":    stage,
			"progress": progress,
			"message":  message,
		},
	})
}

func (n *courseNotifier) CourseGenerationFailed(userID uuid.UUID, course *types.Course, job *types.JobRun, stage string, errorMessage string) {
	n.hub.Broadcast(sse.SSEMessage{
		Channel: userID.String(),
		Event:   sse.CourseGenerationFailed,
		Data: map[string]any{
			"course_id": safeCourseID(course),
			"course":    course,

			"job_id":   safeJobID(job),
			"job_type": safeJobType(job),
			"job":      job,

			"stage": stage,
			"error": errorMessage,
		},
	})
}

func (n *courseNotifier) CourseGenerationDone(userID uuid.UUID, course *types.Course, job *types.JobRun) {
	n.hub.Broadcast(sse.SSEMessage{
		Channel: userID.String(),
		Event:   sse.CourseGenerationDone,
		Data: map[string]any{
			"course_id": safeCourseID(course),
			"course":    course,

			"job_id":   safeJobID(job),
			"job_type": safeJobType(job),
			"job":      job,
		},
	})
}

func safeCourseID(course *types.Course) uuid.UUID {
	if course == nil {
		return uuid.Nil
	}
	return course.ID
}

func safeJobID(job *types.JobRun) uuid.UUID {
	if job == nil {
		return uuid.Nil
	}
	return job.ID
}

func safeJobType(job *types.JobRun) string {
	if job == nil {
		return ""
	}
	return job.JobType
}










