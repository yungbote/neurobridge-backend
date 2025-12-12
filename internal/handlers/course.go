package handlers

import (
      "net/http"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/logger"
	"github.com/yungbote/neurobridge-backend/internal/requestdata"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type CourseHandler struct {
  log       *logger.Logger
  courseSvc services.CourseService
}

func NewCourseHandler(log *logger.Logger, courseSvc services.CourseService) *CourseHandler {
  return &CourseHandler{
    log:       log.With("handler", "CourseHandler"),
    courseSvc: courseSvc,
  }
}

// GET /api/courses
// List this user’s courses with optional filters (status, subject, material_set_id).
func (h *CourseHandler) ListCourses(c *gin.Context) {
  c.Status(http.StatusNotImplemented)
}

// GET /api/courses/:id
// High-level course details and stats.
func (h *CourseHandler) GetCourse(c *gin.Context) {
  c.Status(http.StatusNotImplemented)
}

// GET /api/courses/:id/outline
// Course + modules + lessons structure.
func (h *CourseHandler) GetCourseOutline(c *gin.Context) {
  c.Status(http.StatusNotImplemented)
}

// PATCH /api/courses/:id
// Update course metadata (title, description, tags, level).
func (h *CourseHandler) UpdateCourse(c *gin.Context) {
  c.Status(http.StatusNotImplemented)
}

// POST /api/courses/:id/publish
func (h *CourseHandler) PublishCourse(c *gin.Context) {
  c.Status(http.StatusNotImplemented)
}

// POST /api/courses/:id/unpublish
func (h *CourseHandler) UnpublishCourse(c *gin.Context) {
  c.Status(http.StatusNotImplemented)
}

// POST /api/courses/:id/duplicate
func (h *CourseHandler) DuplicateCourse(c *gin.Context) {
  c.Status(http.StatusNotImplemented)
}

// DELETE /api/courses/:id
func (h *CourseHandler) DeleteCourse(c *gin.Context) {
  c.Status(http.StatusNotImplemented)
}

// GET /api/courses/:id/versions
func (h *CourseHandler) ListCourseVersions(c *gin.Context) {
  c.Status(http.StatusNotImplemented)
}

// GET /api/courses/:id/versions/:versionId
func (h *CourseHandler) GetCourseVersion(c *gin.Context) {
  c.Status(http.StatusNotImplemented)
}










