package handlers

import (
  "net/http"
  "github.com/gin-gonic/gin"
  "github.com/google/uuid"
  "github.com/yungbote/neurobridge-backend/internal/logger"
  "github.com/yungbote/neurobridge-backend/internal/requestdata"
  "github.com/yungbote/neurobridge-backend/internal/services"
)

type TelemetryHandler struct {
  log          *logger.Logger
  telemetrySvc services.TelemetryService
}

func NewTelemetryHandler(log *logger.Logger, tsvc services.TelemetryService) *TelemetryHandler {
  return &TelemetryHandler{
    log:          log.With("handler", "TelemetryHandler"),
    telemetrySvc: tsvc,
  }
}

// POST /api/telemetry/lesson-event
// { lesson_id, type, data }
func (h *TelemetryHandler) RecordLessonEvent(c *gin.Context) {
  c.Status(http.StatusNotImplemented)
}

// POST /api/telemetry/quiz-attempt
func (h *TelemetryHandler) RecordQuizAttempt(c *gin.Context) {
  c.Status(http.StatusNotImplemented)
}

// POST /api/telemetry/feedback
// { lesson_id, type: "too_easy"|"too_hard"|"confusing"|"boring", data }
func (h *TelemetryHandler) RecordFeedback(c *gin.Context) {
  c.Status(http.StatusNotImplemented)
}

// POST /api/telemetry/batch
// Ingest a batch of events at once.
func (h *TelemetryHandler) RecordBatch(c *gin.Context) {
  c.Status(http.StatusNotImplemented)
}










