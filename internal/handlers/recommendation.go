package handlers

import (
  "net/http"
  "github.com/gin-gonic/gin"
  "github.com/google/uuid"
  "github.com/yungbote/neurobridge-backend/internal/logger"
  "github.com/yungbote/neurobridge-backend/internal/requestdata"
  "github.com/yungbote/neurobridge-backend/internal/services"
)

type RecommendationHandler struct {
  log    *logger.Logger
  recSvc services.RecommendationService
}

func NewRecommendationHandler(log *logger.Logger, recSvc services.RecommendationService) *RecommendationHandler {
  return &RecommendationHandler{
    log:    log.With("handler", "RecommendationHandler"),
    recSvc: recSvc,
  }
}


// GET /api/recommendations/next-steps
// High-level next steps: which course/lesson to focus on next.
func (h *RecommendationHandler) GetNextSteps(c *gin.Context) {
  c.Status(http.StatusNotImplemented)
}

// GET /api/recommendations/review
// Recommended review items (lessons/topics) based on mastery/decay.
func (h *RecommendationHandler) GetReviewItems(c *gin.Context) {
  c.Status(http.StatusNotImplemented)
}

// GET /api/recommendations/resources
// Recommended external/internal resources based on profile + goals.
func (h *RecommendationHandler) GetRecommendedResources(c *gin.Context) {
  c.Status(http.StatusNotImplemented)
}










