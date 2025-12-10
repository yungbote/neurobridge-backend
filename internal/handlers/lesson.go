package handlers

import (
  "net/http"
  "github.com/gin-gonic/gin"
  "github.com/google/uuid"
  "github.com/yungbote/neurobridge-backend/internal/logger"
  "github.com/yungbote/neurobridge-backend/internal/requestdata"
  "github.com/yungbote/neurobridge-backend/internal/services"
)

type LessonHandler struct {
  log         *logger.Logger
  lessonSvc   services.LessonService
  progressSvc services.ProgressService
}

func NewLessonHandler(
  log *logger.Logger,
  lessonSvc services.LessonService,
  progressSvc services.ProgressService,
) *LessonHandler {
  return &LessonHandler{
    log:         log.With("handler", "LessonHandler"),
    lessonSvc:   lessonSvc,
    progressSvc: progressSvc,
  }
}

// GET /api/lessons/:id
// Full lesson content (baseline, not runtime-adapted).
func (h *LessonHandler) GetLesson(c *gin.Context) {
  c.Status(http.StatusNotImplemented)
}


// GET /api/courses/:id/lessons
// List all lessons in a course (flattened view).
func (h *LessonHandler) ListCourseLessons(c *gin.Context) {
  c.Status(http.StatusNotImplemented)
}

// PATCH /api/lessons/:id
// Manual edits (title, content_md, metadata).
func (h *LessonHandler) UpdateLesson(c *gin.Context) {
  c.Status(http.StatusNotImplemented)
}

// GET /api/lessons/:id/history
func (h *LessonHandler) GetLessonHistory(c *gin.Context) {
  c.Status(http.StatusNotImplemented)
}

// POST /api/lessons/:id/events
// Record lesson events: opened/completed/abandoned/etc.
func (h *LessonHandler) RecordLessonEvent(c *gin.Context) {
  c.Status(http.StatusNotImplemented)
}


// POST /api/lessons/:id/reorder
// (Optional) reorder lessons inside module/course.
func (h *LessonHandler) ReorderLessons(c *gin.Context) {
  c.Status(http.StatusNotImplemented)
}










