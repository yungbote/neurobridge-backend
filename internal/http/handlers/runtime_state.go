package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/http/response"
	"github.com/yungbote/neurobridge-backend/internal/platform/ctxutil"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
)

type RuntimeStateHandler struct {
	pathRuns repos.PathRunRepo
	nodeRuns repos.NodeRunRepo
	actRuns  repos.ActivityRunRepo
}

func NewRuntimeStateHandler(pathRuns repos.PathRunRepo, nodeRuns repos.NodeRunRepo, actRuns repos.ActivityRunRepo) *RuntimeStateHandler {
	return &RuntimeStateHandler{pathRuns: pathRuns, nodeRuns: nodeRuns, actRuns: actRuns}
}

// GET /api/paths/:id/runtime
func (h *RuntimeStateHandler) GetPathRuntime(c *gin.Context) {
	rd := ctxutil.GetRequestData(c.Request.Context())
	if rd == nil || rd.UserID == uuid.Nil {
		response.RespondError(c, http.StatusUnauthorized, "unauthorized", nil)
		return
	}
	pathID, err := uuid.Parse(c.Param("id"))
	if err != nil || pathID == uuid.Nil {
		response.RespondError(c, http.StatusBadRequest, "invalid_path_id", err)
		return
	}

	dbc := dbctx.Context{Ctx: c.Request.Context()}
	pathRun, _ := h.pathRuns.GetByUserAndPathID(dbc, rd.UserID, pathID)

	var nodeRun any
	var activityRun any
	if pathRun != nil && pathRun.ActiveNodeID != nil && *pathRun.ActiveNodeID != uuid.Nil {
		if nr, _ := h.nodeRuns.GetByUserAndNodeID(dbc, rd.UserID, *pathRun.ActiveNodeID); nr != nil {
			nodeRun = nr
		}
	}
	if pathRun != nil && pathRun.ActiveActivityID != nil && *pathRun.ActiveActivityID != uuid.Nil {
		if ar, _ := h.actRuns.GetByUserAndActivityID(dbc, rd.UserID, *pathRun.ActiveActivityID); ar != nil {
			activityRun = ar
		}
	}

	response.RespondOK(c, gin.H{
		"path_run":     pathRun,
		"node_run":     nodeRun,
		"activity_run": activityRun,
	})
}
