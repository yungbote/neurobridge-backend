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
	log						*logger.Logger
	courseService	services.CourseService
}

func NewCourseHandler(log *logger.Logger, courseService services.CourseService) *CourseHandler {
	return &CourseHandler{
		log:						log.With("handler", "CourseHandler"),
		courseService:	courseService,
	}
}

// GET /api/courses
// Returns all courses for the authenticated user.
func (h *CourseHandler) ListUserCourses(c *gin.Context) {
  rd := requestdata.GetRequestData(c.Request.Context())
  if rd == nil || rd.UserID == uuid.Nil {
    c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
    return
  }

  courses, err := h.courseService.GetUserCourses(c.Request.Context(), nil)
  if err != nil {
    h.log.Error("ListUserCourses failed", "error", err, "user_id", rd.UserID)
    c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load courses"})
    return
  }
  c.JSON(http.StatusOK, gin.H{"courses": courses})
}










