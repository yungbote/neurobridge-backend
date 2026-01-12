package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/http/response"
	"github.com/yungbote/neurobridge-backend/internal/platform/ctxutil"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

type ActivityHandler struct {
	log *logger.Logger

	path             repos.PathRepo
	pathNodes        repos.PathNodeRepo
	pathNodeActivity repos.PathNodeActivityRepo
	activities       repos.ActivityRepo
}

func NewActivityHandler(
	log *logger.Logger,
	path repos.PathRepo,
	pathNodes repos.PathNodeRepo,
	pathNodeActivity repos.PathNodeActivityRepo,
	activities repos.ActivityRepo,
) *ActivityHandler {
	return &ActivityHandler{
		log:              log.With("handler", "ActivityHandler"),
		path:             path,
		pathNodes:        pathNodes,
		pathNodeActivity: pathNodeActivity,
		activities:       activities,
	}
}

// GET /api/activities/:id
func (h *ActivityHandler) GetActivity(c *gin.Context) {
	rd := ctxutil.GetRequestData(c.Request.Context())
	if rd == nil || rd.UserID == uuid.Nil {
		response.RespondError(c, http.StatusUnauthorized, "unauthorized", nil)
		return
	}

	activityID, err := uuid.Parse(c.Param("id"))
	if err != nil || activityID == uuid.Nil {
		response.RespondError(c, http.StatusBadRequest, "invalid_activity_id", err)
		return
	}

	dbc := dbctx.Context{Ctx: c.Request.Context()}
	act, err := h.activities.GetByID(dbc, activityID)
	if err != nil {
		h.log.Error("GetActivity failed (load activity)", "error", err, "activity_id", activityID)
		response.RespondError(c, http.StatusInternalServerError, "load_activity_failed", err)
		return
	}
	if act == nil || act.ID == uuid.Nil || act.OwnerType != "path" || act.OwnerID == nil || *act.OwnerID == uuid.Nil {
		response.RespondError(c, http.StatusNotFound, "activity_not_found", nil)
		return
	}

	pathID := *act.OwnerID
	pathRow, err := h.path.GetByID(dbc, pathID)
	if err != nil {
		h.log.Error("GetActivity failed (load path)", "error", err, "path_id", pathID)
		response.RespondError(c, http.StatusInternalServerError, "load_path_failed", err)
		return
	}
	if pathRow == nil || pathRow.UserID == nil || *pathRow.UserID != rd.UserID {
		response.RespondError(c, http.StatusNotFound, "activity_not_found", nil)
		return
	}

	joins, err := h.pathNodeActivity.GetByActivityIDs(dbc, []uuid.UUID{activityID})
	if err != nil {
		h.log.Error("GetActivity failed (load joins)", "error", err, "activity_id", activityID)
		response.RespondError(c, http.StatusInternalServerError, "load_activity_joins_failed", err)
		return
	}

	var (
		nodeID  uuid.UUID
		nodeRow any
	)
	if len(joins) > 0 && joins[0] != nil {
		nodeID = joins[0].PathNodeID
		if nodeID != uuid.Nil {
			n, err := h.pathNodes.GetByID(dbc, nodeID)
			if err != nil {
				h.log.Error("GetActivity failed (load node)", "error", err, "path_node_id", nodeID)
				response.RespondError(c, http.StatusInternalServerError, "load_node_failed", err)
				return
			}
			// Ownership guard: node must be in the same path.
			if n != nil && n.PathID == pathID {
				nodeRow = n
			}
		}
	}

	response.RespondOK(c, gin.H{
		"activity":     act,
		"path":         pathRow,
		"path_node_id": nodeID,
		"node":         nodeRow,
	})
}
