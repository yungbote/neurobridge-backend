package handlers

import (
  "net/http"
  "github.com/gin-gonic/gin"
  "github.com/google/uuid"
  "github.com/yungbote/neurobridge-backend/internal/logger"
  "github.com/yungbote/neurobridge-backend/internal/requestdata"
  "github.com/yungbote/neurobridge-backend/internal/services"
)

type RuntimeHandler struct {
  log      *logger.Logger
  adaptSvc services.AdaptationService
}

func NewRuntimeHandler(log *logger.Logger, adaptSvc services.AdaptationService) *RuntimeHandler {
  return &RuntimeHandler{
    log:      log.With("handler", "RuntimeHandler"),
    adaptSvc: adaptSvc,
  }
}

// GET /api/runtime/next-lesson
// Next best lesson for this user (within a course or overall).
func (h *RuntimeHandler) GetNextLesson(c *gin.Context) {
  c.Status(http.StatusNotImplemented)
}

// POST /api/runtime/lesson-view
// Given lesson_id, return an adapted view for this user (content + UI hints).
func (h *RuntimeHandler) GetAdaptedLessonView(c *gin.Context) {
  c.Status(http.StatusNotImplemented)
}

// GET /api/runtime/practice-set
// Generate a practice set (spaced repetition / weak topics).
func (h *RuntimeHandler) GetPracticeSet(c *gin.Context) {
  c.Status(http.StatusNotImplemented)
}

// GET /api/runtime/review-plan
// Suggest what to review this week.
func (h *RuntimeHandler) GetReviewPlan(c *gin.Context) {
  c.Status(http.StatusNotImplemented)
}

// POST /api/runtime/preview-adaptation
// Given hypothetical profile changes, preview how content/sequence would change.
func (h *RuntimeHandler) PreviewAdaptation(c *gin.Context) {
  c.Status(http.StatusNotImplemented)
}










