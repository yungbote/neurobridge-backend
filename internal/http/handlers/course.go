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

func (h *CourseHandler) ListUserCourses(c *gin.Context) {
	rd := requestdata.GetRequestData(c.Request.Context())
	if rd == nil || rd.UserID == uuid.Nil {
		RespondError(c, http.StatusUnauthorized, "unauthorized", nil)
		return
	}
	courses, err := h.courseService.GetUserCourses(c.Request.Context(), nil)
	if err != nil {
		h.log.Error("ListUserCourses failed", "error", err, "user_id", rd.UserID)
		RespondError(c, http.StatusInternalServerError, "load_courses_failed", err)
		return
	}
	RespondOK(c, gin.H{"courses": courses})
}










