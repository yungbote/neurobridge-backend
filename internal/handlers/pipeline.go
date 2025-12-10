package handlers

import (
	"net/http"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/logger"
	"github.com/yungbote/neurobridge-backend/internal/requestdata"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type PipelineHandler struct {
	log						*logger.Logger
	pipeline			services.PipelineService
}

func NewPipelineHandler(log *logger.Logger, p services.PipelineService) *PipelineHandler {
	return &PipelineHandler{
		log:				log.With("handler", "PipelineHandler"),
		pipeline:		p,
	}
}

// POST /api/admin/material-sets/:id/analyze
func (h *PipelineHandler) AnalyzeMaterialSet(c *gin.Context) {
	c.Status(http.StatusNotImplemented)
}

// POST /api/admin/material-sets/:id/plan-course
func (h *PipelineHandler) PlanCourse(c *gin.Context) {
	c.Status(http.StatusNotImplemented)
}

// POST /api/admin/material-sets/:id/generate-lessons
func (h *PipelineHandler) GenerateLessonsForCourse(c *gin.Context) {
	c.Status(http.StatusNotImplemented)
}

// POST /api/admin/courses/:id/generate-lesson/:lessonId
func (h *PipelineHandler) RegenerateLesson(c *gin.Context) {
	c.Status(http.StatusNotImplemented)
}

// POST /api/admin/material-sets/:id/run-full
// (Admin entrypoint to run full pipeline in one call)
func (h *PipelineHandler) RunFullPipeline(c *gin.Context) {
	c.Status(http.StatusNotImplemented)
}

// GET /api/admin/material-sets/:id/pipeline-status
func (h *PipelineHandler) GetPipelineStatus(c *gin.Context) {
	c.Status(http.StatusNotImplemented)
}

// POST /api/admin/material-sets/:id/cancel
func (h *PipelineHandler) CancelPipeline(c *gin.Context) {
	c.Status(http.StatusNotImplemented)
}










