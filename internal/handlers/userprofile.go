package handlers

import (
	"net/http"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/logger"
	"github.com/yungbote/neurobridge-backend/internal/requestdata"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type UserProfileHandler struct {
	log								*logger.Logger
	lpSvc							services.LearningProfileService
	masterySvc				services.MasteryService
}

func NewUserProfileHandler(
	log								*logger.Logger,
	lpSvc							services.LearningProfileService,
	masterySvc				services.MasteryService,
) *UserProfileHandler {
	return &UserProfileHandler{
		log:						log.With("handler", "UserProfileHandler"),
		lpSvc:					lpSvc,
		masterSvc:			masterySvc,
	}
}

// GET /api/user/learning-profile
func (h *UserProfileHandler) GetLearningProfile(c *gin.Context) {
	c.Status(http.StatusNotImplemented)
}

// PATCH /api/user/learning-profile
func (h *UserProfileHandler) UpdateLearningProfile(c *gin.Context) {
	c.Status(http.StatusNotImplemented)
}

// POST /api/user/learning-profile/infer
func (h *UserProfileHandler) InferLearningProfile(c *gin.Context) {
	c.Status(http.StatusNotImplemented)
}

// POST /api/user/learning-profile/suggest
// Suggest profile adjustments (diff) without applying.
func (h *UserProfileHandler) SuggestLearningProfileAdjustments(c *gin.Context) {
	c.Status(http.StatusNotImplemented)
}

// GET /api/user/topic-mastery
// Optional filters: course_id, topic, min_mastery, max_mastery
func (h *UserProfileHandler) GetTopicMastery(c *gin.Context) {
	c.Status(http.StatusNotImplemented)
}










