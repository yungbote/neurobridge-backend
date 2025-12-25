package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/realtime"
)

// =========================
// Job notifier
// =========================

type JobNotifier interface {
	JobCreated(userID uuid.UUID, job *types.JobRun)
	JobProgress(userID uuid.UUID, job *types.JobRun, stage string, progress int, message string)
	JobFailed(userID uuid.UUID, job *types.JobRun, stage string, errorMessage string)
	JobDone(userID uuid.UUID, job *types.JobRun)
	JobCanceled(userID uuid.UUID, job *types.JobRun)
	JobRestarted(userID uuid.UUID, job *types.JobRun)
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
	data := map[string]any{"job": job}
	for k, v := range jobLinkData(job) {
		data[k] = v
	}
	n.emit.Emit(context.Background(), realtime.SSEMessage{
		Channel: userID.String(),
		Event:   realtime.SSEEventJobCreated,
		Data:    data,
	})
}

func (n *jobNotifier) JobProgress(userID uuid.UUID, job *types.JobRun, stage string, progress int, message string) {
	if n == nil || n.emit == nil || userID == uuid.Nil {
		return
	}
	data := map[string]any{
		"job_id":   safeJobID(job),
		"job_type": safeJobType(job),
		"stage":    stage,
		"progress": progress,
		"message":  message,
		"job":      job,
	}
	for k, v := range jobLinkData(job) {
		data[k] = v
	}
	n.emit.Emit(context.Background(), realtime.SSEMessage{
		Channel: userID.String(),
		Event:   realtime.SSEEventJobProgress,
		Data:    data,
	})
}

func (n *jobNotifier) JobFailed(userID uuid.UUID, job *types.JobRun, stage string, errorMessage string) {
	if n == nil || n.emit == nil || userID == uuid.Nil {
		return
	}
	data := map[string]any{
		"job_id":   safeJobID(job),
		"job_type": safeJobType(job),
		"stage":    stage,
		"error":    errorMessage,
		"job":      job,
	}
	for k, v := range jobLinkData(job) {
		data[k] = v
	}
	n.emit.Emit(context.Background(), realtime.SSEMessage{
		Channel: userID.String(),
		Event:   realtime.SSEEventJobFailed,
		Data:    data,
	})
}

func (n *jobNotifier) JobDone(userID uuid.UUID, job *types.JobRun) {
	if n == nil || n.emit == nil || userID == uuid.Nil {
		return
	}
	data := map[string]any{
		"job_id":   safeJobID(job),
		"job_type": safeJobType(job),
		"job":      job,
	}
	for k, v := range jobLinkData(job) {
		data[k] = v
	}
	n.emit.Emit(context.Background(), realtime.SSEMessage{
		Channel: userID.String(),
		Event:   realtime.SSEEventJobDone,
		Data:    data,
	})
}

func (n *jobNotifier) JobCanceled(userID uuid.UUID, job *types.JobRun) {
	if n == nil || n.emit == nil || userID == uuid.Nil {
		return
	}
	data := map[string]any{
		"job_id":   safeJobID(job),
		"job_type": safeJobType(job),
		"job":      job,
	}
	for k, v := range jobLinkData(job) {
		data[k] = v
	}
	n.emit.Emit(context.Background(), realtime.SSEMessage{
		Channel: userID.String(),
		Event:   realtime.SSEEventJobCanceled,
		Data:    data,
	})
}

func (n *jobNotifier) JobRestarted(userID uuid.UUID, job *types.JobRun) {
	if n == nil || n.emit == nil || userID == uuid.Nil {
		return
	}
	data := map[string]any{
		"job_id":   safeJobID(job),
		"job_type": safeJobType(job),
		"job":      job,
	}
	for k, v := range jobLinkData(job) {
		data[k] = v
	}
	n.emit.Emit(context.Background(), realtime.SSEMessage{
		Channel: userID.String(),
		Event:   realtime.SSEEventJobRestarted,
		Data:    data,
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
	n.emit.Emit(context.Background(), realtime.SSEMessage{
		Channel: userID.String(),
		Event:   realtime.SSEEventUserCourseCreated,
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
	n.emit.Emit(context.Background(), realtime.SSEMessage{
		Channel: userID.String(),
		Event:   realtime.CourseGenerationProgress,
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
	n.emit.Emit(context.Background(), realtime.SSEMessage{
		Channel: userID.String(),
		Event:   realtime.CourseGenerationFailed,
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
	n.emit.Emit(context.Background(), realtime.SSEMessage{
		Channel: userID.String(),
		Event:   realtime.CourseGenerationDone,
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

func jobLinkData(job *types.JobRun) map[string]any {
	if job == nil || len(job.Payload) == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(job.Payload, &m); err != nil {
		return nil
	}

	out := map[string]any{}

	// Normalize chat thread linkage to "thread_id" for frontend filtering.
	if tid := strings.TrimSpace(fmt.Sprint(m["thread_id"])); tid != "" {
		out["thread_id"] = tid
	} else if tid := strings.TrimSpace(fmt.Sprint(m["chat_thread_id"])); tid != "" {
		out["thread_id"] = tid
	}
	if pid := strings.TrimSpace(fmt.Sprint(m["path_id"])); pid != "" {
		out["path_id"] = pid
	}
	if msid := strings.TrimSpace(fmt.Sprint(m["material_set_id"])); msid != "" {
		out["material_set_id"] = msid
	}

	if len(out) == 0 {
		return nil
	}
	return out
}
