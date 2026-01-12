package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yungbote/neurobridge-backend/internal/http/response"
	learningmod "github.com/yungbote/neurobridge-backend/internal/modules/learning"
	"github.com/yungbote/neurobridge-backend/internal/platform/apierr"
	"github.com/yungbote/neurobridge-backend/internal/platform/ctxutil"
	"strings"
)

// GET /api/path-nodes/:id/drills
func (h *PathHandler) ListPathNodeDrills(c *gin.Context) {
	rd := ctxutil.GetRequestData(c.Request.Context())
	if rd == nil || rd.UserID == uuid.Nil {
		response.RespondError(c, http.StatusUnauthorized, "unauthorized", nil)
		return
	}

	nodeID, err := uuid.Parse(c.Param("id"))
	if err != nil || nodeID == uuid.Nil {
		response.RespondError(c, http.StatusBadRequest, "invalid_path_node_id", err)
		return
	}

	drills, err := h.learning.ListPathNodeDrills(c.Request.Context(), rd.UserID, nodeID)
	if err != nil {
		var ae *apierr.Error
		if errors.As(err, &ae) {
			response.RespondError(c, ae.Status, ae.Code, ae.Err)
			return
		}
		response.RespondError(c, http.StatusInternalServerError, "list_drills_failed", err)
		return
	}

	response.RespondOK(c, gin.H{"drills": drills})
}

type generateDrillRequest struct {
	Count int `json:"count"`
}

// POST /api/path-nodes/:id/drills/:kind
func (h *PathHandler) GeneratePathNodeDrill(c *gin.Context) {
	rd := ctxutil.GetRequestData(c.Request.Context())
	if rd == nil || rd.UserID == uuid.Nil {
		response.RespondError(c, http.StatusUnauthorized, "unauthorized", nil)
		return
	}

	nodeID, err := uuid.Parse(c.Param("id"))
	if err != nil || nodeID == uuid.Nil {
		response.RespondError(c, http.StatusBadRequest, "invalid_path_node_id", err)
		return
	}
	kind := strings.TrimSpace(c.Param("kind"))

	var req generateDrillRequest
	_ = c.ShouldBindJSON(&req)
	if req.Count <= 0 {
		req.Count = 0 // allow prompt defaults
	}

	drill, err := h.learning.GeneratePathNodeDrill(c.Request.Context(), learningmod.GeneratePathNodeDrillInput{
		UserID:     rd.UserID,
		PathNodeID: nodeID,
		Kind:       kind,
		Count:      req.Count,
	})
	if err != nil {
		var ae *apierr.Error
		if errors.As(err, &ae) {
			response.RespondError(c, ae.Status, ae.Code, ae.Err)
			return
		}
		response.RespondError(c, http.StatusInternalServerError, "generate_drill_failed", err)
		return
	}

	response.RespondOK(c, gin.H{"drill": drill})
}
