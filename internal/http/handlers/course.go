package handlers

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/http/response"
	"github.com/yungbote/neurobridge-backend/internal/pkg/ctxutil"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
	"net/http"
)

type CourseHandler struct {
	log           *logger.Logger
	courseService services.CourseService
}

func NewCourseHandler(log *logger.Logger, courseService services.CourseService) *CourseHandler {
	return &CourseHandler{
		log:           log.With("handler", "CourseHandler"),
		courseService: courseService,
	}
}

func (h *CourseHandler) ListUserCourses(c *gin.Context) {
	rd := ctxutil.GetRequestData(c.Request.Context())
	if rd == nil || rd.UserID == uuid.Nil {
		response.RespondError(c, http.StatusUnauthorized, "unauthorized", nil)
		return
	}
	courses, err := h.courseService.GetUserCourses(c.Request.Context(), nil)
	if err != nil {
		h.log.Error("ListUserCourses failed", "error", err, "user_id", rd.UserID)
		response.RespondError(c, http.StatusInternalServerError, "load_courses_failed", err)
		return
	}
	response.RespondOK(c, gin.H{"courses": courses})
}
