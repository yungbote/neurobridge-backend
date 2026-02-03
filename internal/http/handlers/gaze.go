package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yungbote/neurobridge-backend/internal/http/response"
	"github.com/yungbote/neurobridge-backend/internal/platform/ctxutil"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type GazeHandler struct {
	gaze services.GazeService
}

func NewGazeHandler(gaze services.GazeService) *GazeHandler {
	return &GazeHandler{gaze: gaze}
}

type gazeIngestRequest struct {
	PathID string                  `json:"path_id,omitempty"`
	NodeID string                  `json:"node_id,omitempty"`
	Hits   []services.GazeHitInput `json:"hits"`
}

func (h *GazeHandler) Ingest(c *gin.Context) {
	rd := ctxutil.GetRequestData(c.Request.Context())
	if rd == nil || rd.UserID == uuid.Nil {
		response.RespondError(c, http.StatusUnauthorized, "unauthorized", nil)
		return
	}
	if h.gaze == nil {
		response.RespondOK(c, gin.H{"ok": true, "ingested": 0})
		return
	}
	var req gazeIngestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.RespondError(c, http.StatusBadRequest, "invalid_body", err)
		return
	}
	dbc := dbctx.Context{Ctx: c.Request.Context()}
	n, err := h.gaze.Ingest(dbc, rd.UserID, rd.SessionID, services.GazeIngestRequest{
		PathID: req.PathID,
		NodeID: req.NodeID,
		Hits:   req.Hits,
	})
	if err != nil {
		response.RespondError(c, http.StatusBadRequest, "gaze_ingest_failed", err)
		return
	}
	response.RespondOK(c, gin.H{"ok": true, "ingested": n})
}
