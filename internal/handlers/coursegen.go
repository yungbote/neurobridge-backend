package handlers

import (
  "net/http"

  "github.com/gin-gonic/gin"
  "github.com/google/uuid"

  "github.com/yungbote/neurobridge-backend/internal/services"
)

type CourseGenHandler struct {
  svc services.CourseGenStatusService
}

func NewCourseGenHandler(svc services.CourseGenStatusService) *CourseGenHandler {
  return &CourseGenHandler{svc: svc}
}

// GET /api/courses/:id/generation
func (h *CourseGenHandler) GetLatestForCourse(c *gin.Context) {
  courseIDStr := c.Param("id")
  courseID, err := uuid.Parse(courseIDStr)
  if err != nil {
    c.JSON(http.StatusBadRequest, gin.H{"error": "invalid course id"})
    return
  }

  run, err := h.svc.GetLatestRunForCourse(c.Request.Context(), nil, courseID)
  if err != nil {
    c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
    return
  }

  // run can be nil if no runs exist yet
  c.JSON(http.StatusOK, gin.H{"run": run})
}

// GET /api/course-generation-runs/:id
func (h *CourseGenHandler) GetRunByID(c *gin.Context) {
  runIDStr := c.Param("id")
  runID, err := uuid.Parse(runIDStr)
  if err != nil {
    c.JSON(http.StatusBadRequest, gin.H{"error": "invalid run id"})
    return
  }

  run, err := h.svc.GetRunByID(c.Request.Context(), nil, runID)
  if err != nil {
    c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
    return
  }

  c.JSON(http.StatusOK, gin.H{"run": run})
}










