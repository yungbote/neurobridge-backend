package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yungbote/neurobridge-backend/internal/http/response"
	"github.com/yungbote/neurobridge-backend/internal/platform/ctxutil"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

// GET /api/path-nodes/:id/content
func (h *PathHandler) GetPathNodeContent(c *gin.Context) {
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

	node, err := h.pathNodes.GetByID(dbctx.Context{Ctx: c.Request.Context()}, nodeID)
	if err != nil {
		h.log.Error("GetPathNodeContent failed (load node)", "error", err, "path_node_id", nodeID)
		response.RespondError(c, http.StatusInternalServerError, "load_node_failed", err)
		return
	}
	if node == nil || node.PathID == uuid.Nil {
		response.RespondError(c, http.StatusNotFound, "node_not_found", nil)
		return
	}

	pathRow, err := h.path.GetByID(dbctx.Context{Ctx: c.Request.Context()}, node.PathID)
	if err != nil {
		h.log.Error("GetPathNodeContent failed (load path)", "error", err, "path_id", node.PathID)
		response.RespondError(c, http.StatusInternalServerError, "load_path_failed", err)
		return
	}
	if pathRow == nil || pathRow.UserID == nil || *pathRow.UserID != rd.UserID {
		response.RespondError(c, http.StatusNotFound, "path_not_found", nil)
		return
	}
	response.RespondOK(c, gin.H{"node": node})
}
