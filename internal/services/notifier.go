package services

import (
	"context"

	"github.com/google/uuid"

	"github.com/yungbote/neurobridge-backend/internal/sse"
	"github.com/yungbote/neurobridge-backend/internal/types"
)

// =========================
// Job notifier
// =========================

type JobNotifier interface {
	JobCreated(userID uuid.UUID, job *types.JobRun)
	JobProgress(userID uuid.UUID, job *types.JobRun, stage string, progress int, message string)
	JobFailed(userID uuid.UUID, job *types.JobRun, stage string, errorMessage string)
	JobDone(userID uuid.UUID, job *types.JobRun)
}

type jobNotifier struct {
	emit SSEEmitter
}

func NewJobNotifier(emit SSEEmitter) JobNotifier {
	return &jobNotifier{emit: emit}
}

func (n *jobNotifier) JobCreated(userID uuid.UUID, job *types.JobRun) {
	if n == nil || n.emit == nil || userID == uuid.Nil {
		return
	}
	n.emit.Emit(context.Background(), sse.SSEMessage{
		Channel: userID.String(),
		Event:   sse.SSEEventJobCreated,
		Data:    map[string]any{"job": job},
	})
}

func (n *jobNotifier) JobProgress(userID uuid.UUID, job *types.JobRun, stage string, progress int, message string) {
	if n == nil || n.emit == nil || userID == uuid.Nil {
		return
	}
	n.emit.Emit(context.Background(), sse.SSEMessage{
		Channel: userID.String(),
		Event:   sse.SSEEventJobProgress,
		Data: map[string]any{
			"job_id":   safeJobID(job),
			"job_type": safeJobType(job),
			"stage":    stage,
			"progress": progress,
			"message":  message,
			"job":      job,
		},
	})
}

func (n *jobNotifier) JobFailed(userID uuid.UUID, job *types.JobRun, stage string, errorMessage string) {
	if n == nil || n.emit == nil || userID == uuid.Nil {
		return
	}
	n.emit.Emit(context.Background(), sse.SSEMessage{
		Channel: userID.String(),
		Event:   sse.SSEEventJobFailed,
		Data: map[string]any{
			"job_id":   safeJobID(job),
			"job_type": safeJobType(job),
			"stage":    stage,
			"error":    errorMessage,
			"job":      job,
		},
	})
}

func (n *jobNotifier) JobDone(userID uuid.UUID, job *types.JobRun) {
	if n == nil || n.emit == nil || userID == uuid.Nil {
		return
	}
	n.emit.Emit(context.Background(), sse.SSEMessage{
		Channel: userID.String(),
		Event:   sse.SSEEventJobDone,
		Data: map[string]any{
			"job_id":   safeJobID(job),
			"job_type": safeJobType(job),
			"job":      job,
		},
	})
}

// =========================
// Course notifier
// =========================

type CourseNotifier interface {
	CourseCreated(userID uuid.UUID, course *types.Course, job *types.JobRun)

	CourseGenerationProgress(userID uuid.UUID, course *types.Course, job *types.JobRun, stage string, progress int, message string)
	CourseGenerationFailed(userID uuid.UUID, course *types.Course, job *types.JobRun, stage string, errorMessage string)
	CourseGenerationDone(userID uuid.UUID, course *types.Course, job *types.JobRun)
}

type courseNotifier struct {
	emit SSEEmitter
}

func NewCourseNotifier(emit SSEEmitter) CourseNotifier {
	return &courseNotifier{emit: emit}
}

func (n *courseNotifier) CourseCreated(userID uuid.UUID, course *types.Course, job *types.JobRun) {
	if n == nil || n.emit == nil || userID == uuid.Nil {
		return
	}
	n.emit.Emit(context.Background(), sse.SSEMessage{
		Channel: userID.String(),
		Event:   sse.SSEEventUserCourseCreated,
		Data: map[string]any{
			"course": course,
			"job":    job,
		},
	})
}

func (n *courseNotifier) CourseGenerationProgress(userID uuid.UUID, course *types.Course, job *types.JobRun, stage string, progress int, message string) {
	if n == nil || n.emit == nil || userID == uuid.Nil {
		return
	}
	n.emit.Emit(context.Background(), sse.SSEMessage{
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
	if n == nil || n.emit == nil || userID == uuid.Nil {
		return
	}
	n.emit.Emit(context.Background(), sse.SSEMessage{
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
	if n == nil || n.emit == nil || userID == uuid.Nil {
		return
	}
	n.emit.Emit(context.Background(), sse.SSEMessage{
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

// =========================
// helpers
// =========================

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










