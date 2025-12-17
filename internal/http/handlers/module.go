package handlers

import (
  "net/http"

  "github.com/gin-gonic/gin"
  "github.com/google/uuid"

  "github.com/yungbote/neurobridge-backend/internal/services"
)

type ModuleHandler struct {
  svc services.ModuleService
}

func NewModuleHandler(svc services.ModuleService) *ModuleHandler {
  return &ModuleHandler{svc: svc}
}

// GET /api/courses/:id/modules
func (h *ModuleHandler) ListModulesForCourse(c *gin.Context) {
  courseID, err := uuid.Parse(c.Param("id"))
  if err != nil {
    c.JSON(http.StatusBadRequest, gin.H{"error": "invalid course id"})
    return
  }

  modules, err := h.svc.ListModulesForCourse(c.Request.Context(), nil, courseID)
  if err != nil {
    c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
    return
  }

  c.JSON(http.StatusOK, gin.H{"modules": modules})
}










