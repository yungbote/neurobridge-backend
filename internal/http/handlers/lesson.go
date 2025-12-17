package handlers

import (
  "net/http"

  "github.com/gin-gonic/gin"
  "github.com/google/uuid"

  "github.com/yungbote/neurobridge-backend/internal/services"
)

type LessonHandler struct {
  svc   services.LessonService
  jobs  services.JobService
}

func NewLessonHandler(svc services.LessonService, jobs services.JobService) *LessonHandler {
  return &LessonHandler{svc: svc, jobs: jobs}
}

// GET /api/modules/:id/lessons
func (h *LessonHandler) ListModuleLessons(c *gin.Context) {
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

// GET /api/lessons/:id
func (h *LessonHandler) GetLesson(c *gin.Context) {
  lessonID, err := uuid.Parse(c.Param("id"))
  if err != nil {
    c.JSON(http.StatusBadRequest, gin.H{"error": "invalid lesson id"})
    return
  }
  lesson, module, err := h.svc.GetLessonByID(c.Request.Context(), nil, lessonID)
  if err != nil {
    c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
    return
  }
  c.JSON(http.StatusOK, gin.H{
    "lesson": lesson,
    "module": module,
  })
}










