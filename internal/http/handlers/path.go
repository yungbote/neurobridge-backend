package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/http/response"
	"github.com/yungbote/neurobridge-backend/internal/pkg/ctxutil"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
)

type PathHandler struct {
	log *logger.Logger

	path             repos.PathRepo
	pathNodes        repos.PathNodeRepo
	pathNodeActivity repos.PathNodeActivityRepo
	activities       repos.ActivityRepo
}

func NewPathHandler(
	log *logger.Logger,
	path repos.PathRepo,
	pathNodes repos.PathNodeRepo,
	pathNodeActivity repos.PathNodeActivityRepo,
	activities repos.ActivityRepo,
) *PathHandler {
	return &PathHandler{
		log:              log.With("handler", "PathHandler"),
		path:             path,
		pathNodes:        pathNodes,
		pathNodeActivity: pathNodeActivity,
		activities:       activities,
	}
}

// GET /api/paths
func (h *PathHandler) ListUserPaths(c *gin.Context) {
	rd := ctxutil.GetRequestData(c.Request.Context())
	if rd == nil || rd.UserID == uuid.Nil {
		response.RespondError(c, http.StatusUnauthorized, "unauthorized", nil)
		return
	}

	userID := rd.UserID
	paths, err := h.path.ListByUser(c.Request.Context(), nil, &userID)
	if err != nil {
		h.log.Error("ListUserPaths failed", "error", err, "user_id", userID)
		response.RespondError(c, http.StatusInternalServerError, "load_paths_failed", err)
		return
	}

	response.RespondOK(c, gin.H{"paths": paths})
}

// GET /api/paths/:id
func (h *PathHandler) GetPath(c *gin.Context) {
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

	row, err := h.path.GetByID(c.Request.Context(), nil, pathID)
	if err != nil {
		h.log.Error("GetPath failed", "error", err, "path_id", pathID)
		response.RespondError(c, http.StatusInternalServerError, "load_path_failed", err)
		return
	}
	if row == nil || row.UserID == nil || *row.UserID != rd.UserID {
		response.RespondError(c, http.StatusNotFound, "path_not_found", nil)
		return
	}

	response.RespondOK(c, gin.H{"path": row})
}

// GET /api/paths/:id/nodes
func (h *PathHandler) ListPathNodes(c *gin.Context) {
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

	row, err := h.path.GetByID(c.Request.Context(), nil, pathID)
	if err != nil {
		h.log.Error("ListPathNodes failed (load path)", "error", err, "path_id", pathID)
		response.RespondError(c, http.StatusInternalServerError, "load_path_failed", err)
		return
	}
	if row == nil || row.UserID == nil || *row.UserID != rd.UserID {
		response.RespondError(c, http.StatusNotFound, "path_not_found", nil)
		return
	}

	nodes, err := h.pathNodes.GetByPathIDs(c.Request.Context(), nil, []uuid.UUID{pathID})
	if err != nil {
		h.log.Error("ListPathNodes failed (load nodes)", "error", err, "path_id", pathID)
		response.RespondError(c, http.StatusInternalServerError, "load_nodes_failed", err)
		return
	}

	response.RespondOK(c, gin.H{"nodes": nodes})
}

type PathNodeActivityListItem struct {
	ID                 uuid.UUID `json:"id"`
	PathNodeActivityID uuid.UUID `json:"path_node_activity_id"`
	PathNodeID         uuid.UUID `json:"path_node_id"`
	Rank               int       `json:"rank"`
	IsPrimary          bool      `json:"is_primary"`

	Kind             string `json:"kind"`
	Title            string `json:"title"`
	EstimatedMinutes int    `json:"estimated_minutes"`
	Difficulty       string `json:"difficulty,omitempty"`
	Status           string `json:"status"`
}

// GET /api/path-nodes/:id/activities
func (h *PathHandler) ListPathNodeActivities(c *gin.Context) {
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

	node, err := h.pathNodes.GetByID(c.Request.Context(), nil, nodeID)
	if err != nil {
		h.log.Error("ListPathNodeActivities failed (load node)", "error", err, "path_node_id", nodeID)
		response.RespondError(c, http.StatusInternalServerError, "load_node_failed", err)
		return
	}
	if node == nil || node.PathID == uuid.Nil {
		response.RespondError(c, http.StatusNotFound, "node_not_found", nil)
		return
	}

	pathRow, err := h.path.GetByID(c.Request.Context(), nil, node.PathID)
	if err != nil {
		h.log.Error("ListPathNodeActivities failed (load path)", "error", err, "path_id", node.PathID)
		response.RespondError(c, http.StatusInternalServerError, "load_path_failed", err)
		return
	}
	if pathRow == nil || pathRow.UserID == nil || *pathRow.UserID != rd.UserID {
		response.RespondError(c, http.StatusNotFound, "path_not_found", nil)
		return
	}

	joins, err := h.pathNodeActivity.GetByPathNodeIDs(c.Request.Context(), nil, []uuid.UUID{nodeID})
	if err != nil {
		h.log.Error("ListPathNodeActivities failed (load joins)", "error", err, "path_node_id", nodeID)
		response.RespondError(c, http.StatusInternalServerError, "load_path_node_activities_failed", err)
		return
	}
	if len(joins) == 0 {
		response.RespondOK(c, gin.H{"activities": []PathNodeActivityListItem{}})
		return
	}

	activityIDs := make([]uuid.UUID, 0, len(joins))
	for _, j := range joins {
		if j == nil || j.ActivityID == uuid.Nil {
			continue
		}
		activityIDs = append(activityIDs, j.ActivityID)
	}

	activities, err := h.activities.GetByIDs(c.Request.Context(), nil, activityIDs)
	if err != nil {
		h.log.Error("ListPathNodeActivities failed (load activities)", "error", err, "path_node_id", nodeID)
		response.RespondError(c, http.StatusInternalServerError, "load_activities_failed", err)
		return
	}

	// Preserve join order as returned by repo (primary desc, rank asc).
	items := make([]PathNodeActivityListItem, 0, len(joins))
	actByID := make(map[uuid.UUID]*typesActivityProxy, len(activities))
	for _, a := range activities {
		if a == nil || a.ID == uuid.Nil {
			continue
		}
		// Ownership guard: activities for this node must belong to this path.
		if a.OwnerType != "path" || a.OwnerID == nil || *a.OwnerID != node.PathID {
			continue
		}
		actByID[a.ID] = &typesActivityProxy{
			ID:               a.ID,
			Kind:             a.Kind,
			Title:            a.Title,
			EstimatedMinutes: a.EstimatedMinutes,
			Difficulty:       a.Difficulty,
			Status:           a.Status,
		}
	}

	for _, j := range joins {
		if j == nil || j.ActivityID == uuid.Nil {
			continue
		}
		act := actByID[j.ActivityID]
		if act == nil {
			continue
		}
		items = append(items, PathNodeActivityListItem{
			ID:                 act.ID,
			PathNodeActivityID: j.ID,
			PathNodeID:         j.PathNodeID,
			Rank:               j.Rank,
			IsPrimary:          j.IsPrimary,
			Kind:               act.Kind,
			Title:              act.Title,
			EstimatedMinutes:   act.EstimatedMinutes,
			Difficulty:         act.Difficulty,
			Status:             act.Status,
		})
	}

	response.RespondOK(c, gin.H{"activities": items})
}

// Lightweight proxies so this handler doesn't need to depend on full domain types for response shaping.
// (We intentionally keep the response schema small and stable for the frontend.)
type typesActivityProxy struct {
	ID               uuid.UUID
	Kind             string
	Title            string
	EstimatedMinutes int
	Difficulty       string
	Status           string
}
