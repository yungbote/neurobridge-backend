package handlers

import (
  "net/http"

  "github.com/gin-gonic/gin"
  "github.com/google/uuid"

  "github.com/yungbote/neurobridge-backend/internal/services"
)

type LessonHandler struct {
  svc services.LessonService
}

func NewLessonHandler(svc services.LessonService) *LessonHandler {
  return &LessonHandler{svc: svc}
}

// GET /api/modules/:id/lessons
func (h *LessonHandler) ListLessonsForModule(c *gin.Context) {
  moduleID, err := uuid.Parse(c.Param("id"))
  if err != nil {
    c.JSON(http.StatusBadRequest, gin.H{"error": "invalid module id"})
    return
  }

  lessons, err := h.svc.ListLessonsForModule(c.Request.Context(), nil, moduleID)
  if err != nil {
    c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
    return
  }

  c.JSON(http.StatusOK, gin.H{"lessons": lessons})
}










