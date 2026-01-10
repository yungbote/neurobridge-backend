package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/yungbote/neurobridge-backend/internal/clients/gcp"
	"github.com/yungbote/neurobridge-backend/internal/clients/openai"
	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/http/response"
	"github.com/yungbote/neurobridge-backend/internal/jobs/learning/steps"
	"github.com/yungbote/neurobridge-backend/internal/learning/content"
	"github.com/yungbote/neurobridge-backend/internal/learning/content/schema"
	"github.com/yungbote/neurobridge-backend/internal/pkg/ctxutil"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/pkg/envutil"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type PathHandler struct {
	log *logger.Logger
	db  *gorm.DB

	path             repos.PathRepo
	pathNodes        repos.PathNodeRepo
	pathNodeActivity repos.PathNodeActivityRepo
	activities       repos.ActivityRepo
	nodeDocs         repos.LearningNodeDocRepo
	docRevisions     repos.LearningNodeDocRevisionRepo
	drillInstances   repos.LearningDrillInstanceRepo
	genRuns          repos.LearningDocGenerationRunRepo
	chunks           repos.MaterialChunkRepo
	materialSets     repos.MaterialSetRepo
	materialFiles    repos.MaterialFileRepo
	materialAssets   repos.MaterialAssetRepo
	userLibraryIndex repos.UserLibraryIndexRepo

	concepts repos.ConceptRepo
	edges    repos.ConceptEdgeRepo

	assets repos.AssetRepo
	jobs   repos.JobRunRepo
	jobSvc services.JobService

	avatar      services.AvatarService
	userProfile repos.UserProfileVectorRepo
	ai          openai.Client
	bucket      gcp.BucketService
}

func NewPathHandler(
	log *logger.Logger,
	db *gorm.DB,
	path repos.PathRepo,
	pathNodes repos.PathNodeRepo,
	pathNodeActivity repos.PathNodeActivityRepo,
	activities repos.ActivityRepo,
	nodeDocs repos.LearningNodeDocRepo,
	docRevisions repos.LearningNodeDocRevisionRepo,
	drillInstances repos.LearningDrillInstanceRepo,
	genRuns repos.LearningDocGenerationRunRepo,
	chunks repos.MaterialChunkRepo,
	materialSets repos.MaterialSetRepo,
	materialFiles repos.MaterialFileRepo,
	materialAssets repos.MaterialAssetRepo,
	userLibraryIndex repos.UserLibraryIndexRepo,
	concepts repos.ConceptRepo,
	edges repos.ConceptEdgeRepo,
	assets repos.AssetRepo,
	jobs repos.JobRunRepo,
	jobSvc services.JobService,
	avatar services.AvatarService,
	userProfile repos.UserProfileVectorRepo,
	ai openai.Client,
	bucket gcp.BucketService,
) *PathHandler {
	return &PathHandler{
		log:              log.With("handler", "PathHandler"),
		db:               db,
		path:             path,
		pathNodes:        pathNodes,
		pathNodeActivity: pathNodeActivity,
		activities:       activities,
		nodeDocs:         nodeDocs,
		docRevisions:     docRevisions,
		drillInstances:   drillInstances,
		genRuns:          genRuns,
		chunks:           chunks,
		materialSets:     materialSets,
		materialFiles:    materialFiles,
		materialAssets:   materialAssets,
		userLibraryIndex: userLibraryIndex,
		concepts:         concepts,
		edges:            edges,
		assets:           assets,
		jobs:             jobs,
		jobSvc:           jobSvc,
		avatar:           avatar,
		userProfile:      userProfile,
		ai:               ai,
		bucket:           bucket,
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
	paths, err := h.path.ListByUser(dbctx.Context{Ctx: c.Request.Context()}, &userID)
	if err != nil {
		h.log.Error("ListUserPaths failed", "error", err, "user_id", userID)
		response.RespondError(c, http.StatusInternalServerError, "load_paths_failed", err)
		return
	}

	// Refresh-safe UX: include a durable job snapshot (status/stage/progress/message) for any path with job_id.
	pathsWithJobs := h.attachJobSnapshot(c.Request.Context(), rd.UserID, paths)

	response.RespondOK(c, gin.H{"paths": pathsWithJobs})
}

// DELETE /api/paths/:id
func (h *PathHandler) DeletePath(c *gin.Context) {
	rd := ctxutil.GetRequestData(c.Request.Context())
	if rd == nil || rd.UserID == uuid.Nil {
		response.RespondError(c, http.StatusUnauthorized, "unauthorized", nil)
		return
	}
	if h.path == nil || h.userLibraryIndex == nil {
		response.RespondError(c, http.StatusInternalServerError, "path_repo_missing", nil)
		return
	}

	pathID, err := uuid.Parse(c.Param("id"))
	if err != nil || pathID == uuid.Nil {
		response.RespondError(c, http.StatusBadRequest, "invalid_path_id", err)
		return
	}

	row, err := h.path.GetByID(dbctx.Context{Ctx: c.Request.Context()}, pathID)
	if err != nil {
		h.log.Error("DeletePath failed (load path)", "error", err, "path_id", pathID)
		response.RespondError(c, http.StatusInternalServerError, "load_path_failed", err)
		return
	}
	if row == nil || row.UserID == nil || *row.UserID != rd.UserID {
		response.RespondError(c, http.StatusNotFound, "path_not_found", nil)
		return
	}

	idx, err := h.userLibraryIndex.GetByUserAndPathID(dbctx.Context{Ctx: c.Request.Context()}, rd.UserID, pathID)
	if err != nil {
		h.log.Error("DeletePath failed (load library index)", "error", err, "path_id", pathID)
		response.RespondError(c, http.StatusInternalServerError, "load_library_index_failed", err)
		return
	}

	var (
		idxID         uuid.UUID
		materialSetID uuid.UUID
	)
	if idx != nil && idx.ID != uuid.Nil {
		idxID = idx.ID
		materialSetID = idx.MaterialSetID
	}

	if h.db == nil {
		response.RespondError(c, http.StatusInternalServerError, "db_unavailable", nil)
		return
	}

	if err := h.db.WithContext(c.Request.Context()).Transaction(func(txx *gorm.DB) error {
		dbc := dbctx.Context{Ctx: c.Request.Context(), Tx: txx}

		// Delete the index row first so we never keep a dangling (user, material_set)->path_id mapping.
		if idxID != uuid.Nil {
			if err := h.userLibraryIndex.FullDeleteByIDs(dbc, []uuid.UUID{idxID}); err != nil {
				return err
			}
		}
		if materialSetID != uuid.Nil && h.materialSets != nil {
			if err := h.materialSets.FullDeleteByIDs(dbc, []uuid.UUID{materialSetID}); err != nil {
				return err
			}
		}
		if err := h.path.FullDeleteByIDs(dbc, []uuid.UUID{pathID}); err != nil {
			return err
		}
		return nil
	}); err != nil {
		h.log.Error("DeletePath failed (transaction)", "error", err, "path_id", pathID)
		response.RespondError(c, http.StatusInternalServerError, "delete_path_failed", err)
		return
	}

	// Best-effort: delete material objects from bucket (db delete already succeeded).
	if materialSetID != uuid.Nil && h.bucket != nil {
		prefix := fmt.Sprintf("materials/%s/", materialSetID.String())
		_ = h.bucket.DeletePrefix(c.Request.Context(), gcp.BucketCategoryMaterial, prefix)
	}

	response.RespondOK(c, gin.H{"ok": true})
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

	row, err := h.path.GetByID(dbctx.Context{Ctx: c.Request.Context()}, pathID)
	if err != nil {
		h.log.Error("GetPath failed", "error", err, "path_id", pathID)
		response.RespondError(c, http.StatusInternalServerError, "load_path_failed", err)
		return
	}
	if row == nil || row.UserID == nil || *row.UserID != rd.UserID {
		response.RespondError(c, http.StatusNotFound, "path_not_found", nil)
		return
	}

	dto := &pathWithJob{Path: row}
	if h.jobs != nil && row.JobID != nil && *row.JobID != uuid.Nil {
		jobs, err := h.jobs.GetByIDs(dbctx.Context{Ctx: c.Request.Context()}, []uuid.UUID{*row.JobID})
		if err == nil && len(jobs) > 0 && jobs[0] != nil && jobs[0].OwnerUserID == rd.UserID {
			dto.JobStatus = jobs[0].Status
			dto.JobStage = jobs[0].Stage
			dto.JobProgress = jobs[0].Progress
			dto.JobMessage = jobs[0].Message
		}
	}
	if h.userLibraryIndex != nil {
		idx, err := h.userLibraryIndex.GetByUserAndPathID(dbctx.Context{Ctx: c.Request.Context()}, rd.UserID, pathID)
		if err == nil && idx != nil && idx.MaterialSetID != uuid.Nil {
			msid := idx.MaterialSetID
			dto.MaterialSetID = &msid
		}
	}

	response.RespondOK(c, gin.H{"path": dto})
}

// POST /api/paths/:id/view
func (h *PathHandler) ViewPath(c *gin.Context) {
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

	dedupeSeconds := envutil.Int("PATH_VIEW_DEDUPE_SECONDS", 10)
	if dedupeSeconds < 0 {
		dedupeSeconds = 0
	}

	_, _, ok, err := h.path.RecordView(dbctx.Context{Ctx: c.Request.Context()}, rd.UserID, pathID, time.Duration(dedupeSeconds)*time.Second)
	if err != nil {
		h.log.Error("ViewPath failed (record view)", "error", err, "path_id", pathID)
		response.RespondError(c, http.StatusInternalServerError, "record_view_failed", err)
		return
	}
	if !ok {
		response.RespondError(c, http.StatusNotFound, "path_not_found", nil)
		return
	}

	row, err := h.path.GetByID(dbctx.Context{Ctx: c.Request.Context()}, pathID)
	if err != nil {
		h.log.Error("ViewPath failed (reload path)", "error", err, "path_id", pathID)
		response.RespondError(c, http.StatusInternalServerError, "load_path_failed", err)
		return
	}
	if row == nil || row.UserID == nil || *row.UserID != rd.UserID {
		response.RespondError(c, http.StatusNotFound, "path_not_found", nil)
		return
	}

	dto := &pathWithJob{Path: row}
	if h.jobs != nil && row.JobID != nil && *row.JobID != uuid.Nil {
		jobs, err := h.jobs.GetByIDs(dbctx.Context{Ctx: c.Request.Context()}, []uuid.UUID{*row.JobID})
		if err == nil && len(jobs) > 0 && jobs[0] != nil && jobs[0].OwnerUserID == rd.UserID {
			dto.JobStatus = jobs[0].Status
			dto.JobStage = jobs[0].Stage
			dto.JobProgress = jobs[0].Progress
			dto.JobMessage = jobs[0].Message
		}
	}
	if h.userLibraryIndex != nil {
		idx, err := h.userLibraryIndex.GetByUserAndPathID(dbctx.Context{Ctx: c.Request.Context()}, rd.UserID, pathID)
		if err == nil && idx != nil && idx.MaterialSetID != uuid.Nil {
			msid := idx.MaterialSetID
			dto.MaterialSetID = &msid
		}
	}

	response.RespondOK(c, gin.H{"path": dto})
}

type pathCoverRequest struct {
	Force bool `json:"force"`
}

// POST /api/paths/:id/cover
func (h *PathHandler) GeneratePathCover(c *gin.Context) {
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

	row, err := h.path.GetByID(dbctx.Context{Ctx: c.Request.Context()}, pathID)
	if err != nil {
		h.log.Error("GeneratePathCover failed (load path)", "error", err, "path_id", pathID)
		response.RespondError(c, http.StatusInternalServerError, "load_path_failed", err)
		return
	}
	if row == nil || row.UserID == nil || *row.UserID != rd.UserID {
		response.RespondError(c, http.StatusNotFound, "path_not_found", nil)
		return
	}

	req := pathCoverRequest{}
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		response.RespondError(c, http.StatusBadRequest, "invalid_body", err)
		return
	}

	force := req.Force
	if !force {
		q := strings.TrimSpace(c.Query("force"))
		if q != "" {
			q = strings.ToLower(q)
			force = q == "1" || q == "true" || q == "yes"
		}
	}

	out, err := steps.PathCoverRender(c.Request.Context(), steps.PathCoverRenderDeps{
		Log:       h.log,
		Path:      h.path,
		PathNodes: h.pathNodes,
		Avatar:    h.avatar,
	}, steps.PathCoverRenderInput{PathID: pathID, Force: force})
	if err != nil {
		h.log.Error("GeneratePathCover failed (render)", "error", err, "path_id", pathID)
		response.RespondError(c, http.StatusInternalServerError, "generate_path_cover_failed", err)
		return
	}

	updated, err := h.path.GetByID(dbctx.Context{Ctx: c.Request.Context()}, pathID)
	if err != nil {
		h.log.Error("GeneratePathCover failed (reload path)", "error", err, "path_id", pathID)
		response.RespondError(c, http.StatusInternalServerError, "load_path_failed", err)
		return
	}

	dto := &pathWithJob{Path: updated}
	response.RespondOK(c, gin.H{"path": dto, "cover": out})
}

// GET /api/paths/:id/materials
func (h *PathHandler) ListPathMaterials(c *gin.Context) {
	rd := ctxutil.GetRequestData(c.Request.Context())
	if rd == nil || rd.UserID == uuid.Nil {
		response.RespondError(c, http.StatusUnauthorized, "unauthorized", nil)
		return
	}
	if h.materialFiles == nil || h.userLibraryIndex == nil {
		response.RespondError(c, http.StatusInternalServerError, "material_repo_missing", nil)
		return
	}

	pathID, err := uuid.Parse(c.Param("id"))
	if err != nil || pathID == uuid.Nil {
		response.RespondError(c, http.StatusBadRequest, "invalid_path_id", err)
		return
	}

	pathRow, err := h.path.GetByID(dbctx.Context{Ctx: c.Request.Context()}, pathID)
	if err != nil {
		h.log.Error("ListPathMaterials failed (load path)", "error", err, "path_id", pathID)
		response.RespondError(c, http.StatusInternalServerError, "load_path_failed", err)
		return
	}
	if pathRow == nil || pathRow.UserID == nil || *pathRow.UserID != rd.UserID {
		response.RespondError(c, http.StatusNotFound, "path_not_found", nil)
		return
	}

	idxRow, err := h.userLibraryIndex.GetByUserAndPathID(dbctx.Context{Ctx: c.Request.Context()}, rd.UserID, pathID)
	if err != nil {
		h.log.Error("ListPathMaterials failed (load library index)", "error", err, "path_id", pathID)
		response.RespondError(c, http.StatusInternalServerError, "load_library_index_failed", err)
		return
	}
	if idxRow == nil || idxRow.MaterialSetID == uuid.Nil {
		response.RespondOK(c, gin.H{"files": []any{}, "assets": []any{}, "assets_by_file": gin.H{}})
		return
	}

	files, err := h.materialFiles.GetByMaterialSetID(dbctx.Context{Ctx: c.Request.Context()}, idxRow.MaterialSetID)
	if err != nil {
		h.log.Error("ListPathMaterials failed (load files)", "error", err, "material_set_id", idxRow.MaterialSetID)
		response.RespondError(c, http.StatusInternalServerError, "load_files_failed", err)
		return
	}

	fileIDs := make([]uuid.UUID, 0, len(files))
	for _, f := range files {
		if f == nil || f.ID == uuid.Nil {
			continue
		}
		fileIDs = append(fileIDs, f.ID)
	}

	assets := []*types.MaterialAsset{}
	assetsByFile := map[string][]*types.MaterialAsset{}
	if h.materialAssets != nil && len(fileIDs) > 0 {
		rows, err := h.materialAssets.GetByMaterialFileIDs(dbctx.Context{Ctx: c.Request.Context()}, fileIDs)
		if err != nil {
			h.log.Error("ListPathMaterials failed (load assets)", "error", err, "material_set_id", idxRow.MaterialSetID)
			response.RespondError(c, http.StatusInternalServerError, "load_assets_failed", err)
			return
		}
		assets = rows
		for _, a := range rows {
			if a == nil || a.MaterialFileID == uuid.Nil {
				continue
			}
			key := a.MaterialFileID.String()
			assetsByFile[key] = append(assetsByFile[key], a)
		}
	}

	response.RespondOK(c, gin.H{
		"files":           files,
		"assets":          assets,
		"assets_by_file":  assetsByFile,
		"material_set_id": idxRow.MaterialSetID.String(),
	})
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

	row, err := h.path.GetByID(dbctx.Context{Ctx: c.Request.Context()}, pathID)
	if err != nil {
		h.log.Error("ListPathNodes failed (load path)", "error", err, "path_id", pathID)
		response.RespondError(c, http.StatusInternalServerError, "load_path_failed", err)
		return
	}
	if row == nil || row.UserID == nil || *row.UserID != rd.UserID {
		response.RespondError(c, http.StatusNotFound, "path_not_found", nil)
		return
	}

	nodes, err := h.pathNodes.GetByPathIDs(dbctx.Context{Ctx: c.Request.Context()}, []uuid.UUID{pathID})
	if err != nil {
		h.log.Error("ListPathNodes failed (load nodes)", "error", err, "path_id", pathID)
		response.RespondError(c, http.StatusInternalServerError, "load_nodes_failed", err)
		return
	}
	response.RespondOK(c, gin.H{"nodes": nodes})
}

// GET /api/paths/:id/concept-graph
func (h *PathHandler) GetConceptGraph(c *gin.Context) {
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

	row, err := h.path.GetByID(dbctx.Context{Ctx: c.Request.Context()}, pathID)
	if err != nil {
		h.log.Error("GetConceptGraph failed (load path)", "error", err, "path_id", pathID)
		response.RespondError(c, http.StatusInternalServerError, "load_path_failed", err)
		return
	}
	if row == nil || row.UserID == nil || *row.UserID != rd.UserID {
		response.RespondError(c, http.StatusNotFound, "path_not_found", nil)
		return
	}

	concepts, err := h.concepts.GetByScope(dbctx.Context{Ctx: c.Request.Context()}, "path", &pathID)
	if err != nil {
		h.log.Error("GetConceptGraph failed (load concepts)", "error", err, "path_id", pathID)
		response.RespondError(c, http.StatusInternalServerError, "load_concepts_failed", err)
		return
	}
	if len(concepts) == 0 {
		response.RespondOK(c, gin.H{"concepts": []any{}, "edges": []any{}})
		return
	}

	ids := make([]uuid.UUID, 0, len(concepts))
	idSet := make(map[uuid.UUID]bool, len(concepts))
	for _, cc := range concepts {
		if cc == nil || cc.ID == uuid.Nil {
			continue
		}
		ids = append(ids, cc.ID)
		idSet[cc.ID] = true
	}

	edges, err := h.edges.GetByConceptIDs(dbctx.Context{Ctx: c.Request.Context()}, ids)
	if err != nil {
		h.log.Error("GetConceptGraph failed (load edges)", "error", err, "path_id", pathID)
		response.RespondError(c, http.StatusInternalServerError, "load_edges_failed", err)
		return
	}

	// Only return edges that stay within this path's concept set.
	filtered := make([]any, 0, len(edges))
	for _, e := range edges {
		if e == nil {
			continue
		}
		if !idSet[e.FromConceptID] || !idSet[e.ToConceptID] {
			continue
		}
		filtered = append(filtered, e)
	}

	response.RespondOK(c, gin.H{"concepts": concepts, "edges": filtered})
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

	node, err := h.pathNodes.GetByID(dbctx.Context{Ctx: c.Request.Context()}, nodeID)
	if err != nil {
		h.log.Error("ListPathNodeActivities failed (load node)", "error", err, "path_node_id", nodeID)
		response.RespondError(c, http.StatusInternalServerError, "load_node_failed", err)
		return
	}
	if node == nil || node.PathID == uuid.Nil {
		response.RespondError(c, http.StatusNotFound, "node_not_found", nil)
		return
	}

	pathRow, err := h.path.GetByID(dbctx.Context{Ctx: c.Request.Context()}, node.PathID)
	if err != nil {
		h.log.Error("ListPathNodeActivities failed (load path)", "error", err, "path_id", node.PathID)
		response.RespondError(c, http.StatusInternalServerError, "load_path_failed", err)
		return
	}
	if pathRow == nil || pathRow.UserID == nil || *pathRow.UserID != rd.UserID {
		response.RespondError(c, http.StatusNotFound, "path_not_found", nil)
		return
	}

	joins, err := h.pathNodeActivity.GetByPathNodeIDs(dbctx.Context{Ctx: c.Request.Context()}, []uuid.UUID{nodeID})
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

	activities, err := h.activities.GetByIDs(dbctx.Context{Ctx: c.Request.Context()}, activityIDs)
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

// ---------------- Node-first content + inline drills ----------------

type pathWithJob struct {
	*types.Path
	JobStatus   string `json:"job_status,omitempty"`
	JobStage    string `json:"job_stage,omitempty"`
	JobProgress int    `json:"job_progress,omitempty"`
	JobMessage  string `json:"job_message,omitempty"`

	// Derived from user_library_index (Path doesn't store this directly).
	MaterialSetID *uuid.UUID `json:"material_set_id,omitempty"`
}

func (h *PathHandler) attachJobSnapshot(ctx context.Context, userID uuid.UUID, paths []*types.Path) []*pathWithJob {
	if h == nil {
		return nil
	}
	out := make([]*pathWithJob, 0, len(paths))
	if len(paths) == 0 {
		return out
	}

	materialSetByPathID := map[uuid.UUID]uuid.UUID{}
	if h.userLibraryIndex != nil {
		rows, err := h.userLibraryIndex.GetByUserIDs(dbctx.Context{Ctx: ctx}, []uuid.UUID{userID})
		if err == nil {
			for _, r := range rows {
				if r == nil || r.PathID == nil || *r.PathID == uuid.Nil || r.MaterialSetID == uuid.Nil {
					continue
				}
				materialSetByPathID[*r.PathID] = r.MaterialSetID
			}
		}
	}

	jobIDs := make([]uuid.UUID, 0, len(paths))
	for _, p := range paths {
		if p == nil || p.JobID == nil || *p.JobID == uuid.Nil {
			continue
		}
		jobIDs = append(jobIDs, *p.JobID)
	}

	jobByID := map[uuid.UUID]*types.JobRun{}
	if h.jobs != nil && len(jobIDs) > 0 {
		rows, err := h.jobs.GetByIDs(dbctx.Context{Ctx: ctx}, jobIDs)
		if err == nil {
			for _, j := range rows {
				if j == nil || j.ID == uuid.Nil || j.OwnerUserID != userID {
					continue
				}
				jobByID[j.ID] = j
			}
		}
	}

	for _, p := range paths {
		if p == nil {
			continue
		}
		dto := &pathWithJob{Path: p}

		if msid, ok := materialSetByPathID[p.ID]; ok && msid != uuid.Nil {
			v := msid
			dto.MaterialSetID = &v
		}

		if p.JobID != nil && *p.JobID != uuid.Nil {
			if j := jobByID[*p.JobID]; j != nil {
				dto.JobStatus = j.Status
				dto.JobStage = j.Stage
				dto.JobProgress = j.Progress
				dto.JobMessage = j.Message
			}
		}
		out = append(out, dto)
	}

	return out
}

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

// GET /api/path-nodes/:id/doc
func (h *PathHandler) GetPathNodeDoc(c *gin.Context) {
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
		h.log.Error("GetPathNodeDoc failed (load node)", "error", err, "path_node_id", nodeID)
		response.RespondError(c, http.StatusInternalServerError, "load_node_failed", err)
		return
	}
	if node == nil || node.PathID == uuid.Nil {
		response.RespondError(c, http.StatusNotFound, "node_not_found", nil)
		return
	}

	pathRow, err := h.path.GetByID(dbctx.Context{Ctx: c.Request.Context()}, node.PathID)
	if err != nil {
		h.log.Error("GetPathNodeDoc failed (load path)", "error", err, "path_id", node.PathID)
		response.RespondError(c, http.StatusInternalServerError, "load_path_failed", err)
		return
	}
	if pathRow == nil || pathRow.UserID == nil || *pathRow.UserID != rd.UserID {
		response.RespondError(c, http.StatusNotFound, "path_not_found", nil)
		return
	}

	docRow, err := h.nodeDocs.GetByPathNodeID(dbctx.Context{Ctx: c.Request.Context()}, nodeID)
	if err != nil {
		h.log.Error("GetPathNodeDoc failed (load doc)", "error", err, "path_node_id", nodeID)
		response.RespondError(c, http.StatusInternalServerError, "load_doc_failed", err)
		return
	}
	if docRow == nil || len(docRow.DocJSON) == 0 || string(docRow.DocJSON) == "null" {
		response.RespondError(c, http.StatusNotFound, "doc_not_found", nil)
		return
	}

	var doc content.NodeDocV1
	if err := json.Unmarshal(docRow.DocJSON, &doc); err != nil {
		response.RespondError(c, http.StatusInternalServerError, "doc_invalid_json", err)
		return
	}

	if withIDs, changed := content.EnsureNodeDocBlockIDs(doc); changed {
		doc = withIDs
		if rawDoc, err := json.Marshal(doc); err == nil {
			if canon, cErr := content.CanonicalizeJSON(rawDoc); cErr == nil {
				now := time.Now().UTC()
				updated := &types.LearningNodeDoc{
					ID:            docRow.ID,
					UserID:        docRow.UserID,
					PathID:        docRow.PathID,
					PathNodeID:    docRow.PathNodeID,
					SchemaVersion: docRow.SchemaVersion,
					DocJSON:       datatypes.JSON(canon),
					DocText:       docRow.DocText,
					ContentHash:   content.HashBytes(canon),
					SourcesHash:   docRow.SourcesHash,
					CreatedAt:     docRow.CreatedAt,
					UpdatedAt:     now,
				}
				_ = h.nodeDocs.Upsert(dbctx.Context{Ctx: c.Request.Context()}, updated)
			}
		}
	}

	// Generated figures are stored in a private bucket; rewrite figure URLs to a protected streaming endpoint.
	// This avoids mixed public/private bucket configs and prevents stale/signed URLs from breaking the UI.
	if withAssetURLs, changed := h.rewriteNodeDocFigureAssetURLs(doc, nodeID); changed {
		doc = withAssetURLs
	}

	response.RespondOK(c, gin.H{"doc": doc})
}

// GET /api/path-nodes/:id/assets/view?key=...
func (h *PathHandler) ViewPathNodeAsset(c *gin.Context) {
	rd := ctxutil.GetRequestData(c.Request.Context())
	if rd == nil || rd.UserID == uuid.Nil {
		response.RespondError(c, http.StatusUnauthorized, "unauthorized", nil)
		return
	}
	if h.pathNodes == nil || h.path == nil {
		response.RespondError(c, http.StatusInternalServerError, "path_repo_missing", nil)
		return
	}
	if h.bucket == nil {
		response.RespondError(c, http.StatusInternalServerError, "bucket_unavailable", nil)
		return
	}

	nodeID, err := uuid.Parse(c.Param("id"))
	if err != nil || nodeID == uuid.Nil {
		response.RespondError(c, http.StatusBadRequest, "invalid_path_node_id", err)
		return
	}
	storageKey := strings.TrimSpace(c.Query("key"))
	if storageKey == "" {
		response.RespondError(c, http.StatusBadRequest, "missing_storage_key", nil)
		return
	}

	node, err := h.pathNodes.GetByID(dbctx.Context{Ctx: c.Request.Context()}, nodeID)
	if err != nil {
		h.log.Error("ViewPathNodeAsset failed (load node)", "error", err, "path_node_id", nodeID)
		response.RespondError(c, http.StatusInternalServerError, "load_node_failed", err)
		return
	}
	if node == nil || node.PathID == uuid.Nil {
		response.RespondError(c, http.StatusNotFound, "node_not_found", nil)
		return
	}

	pathRow, err := h.path.GetByID(dbctx.Context{Ctx: c.Request.Context()}, node.PathID)
	if err != nil {
		h.log.Error("ViewPathNodeAsset failed (load path)", "error", err, "path_id", node.PathID)
		response.RespondError(c, http.StatusInternalServerError, "load_path_failed", err)
		return
	}
	if pathRow == nil || pathRow.UserID == nil || *pathRow.UserID != rd.UserID {
		response.RespondError(c, http.StatusNotFound, "path_not_found", nil)
		return
	}

	// Prevent arbitrary bucket reads: only allow generated node figure assets for this node.
	allowedPrefix := fmt.Sprintf("generated/node_figures/%s/%s/", node.PathID.String(), node.ID.String())
	if !strings.HasPrefix(storageKey, allowedPrefix) {
		response.RespondError(c, http.StatusNotFound, "asset_not_found", nil)
		return
	}

	ctx := c.Request.Context()
	attrs, err := h.bucket.GetObjectAttrs(ctx, gcp.BucketCategoryMaterial, storageKey)
	if err != nil {
		h.log.Error("ViewPathNodeAsset failed (GetObjectAttrs)", "error", err, "storage_key", storageKey)
		response.RespondError(c, http.StatusNotFound, "asset_not_found", err)
		return
	}

	contentType := resolveContentType("", attrs.ContentType, storageKey, storageKey)
	disposition := buildContentDisposition(storageKey, c.Query("download") != "")
	size := attrs.Size
	rangeHeader := c.GetHeader("Range")

	if rangeHeader != "" && size > 0 {
		rng, ok, rErr := parseByteRangeHeader(rangeHeader, size)
		if rErr != nil {
			c.Header("Content-Range", fmt.Sprintf("bytes */%d", size))
			response.RespondError(c, http.StatusRequestedRangeNotSatisfiable, "invalid_range", rErr)
			return
		}
		if ok {
			reader, err := h.bucket.OpenRangeReader(ctx, gcp.BucketCategoryMaterial, storageKey, rng.start, rng.end-rng.start+1)
			if err != nil {
				h.log.Error("ViewPathNodeAsset failed (OpenRangeReader)", "error", err, "storage_key", storageKey)
				response.RespondError(c, http.StatusInternalServerError, "stream_failed", err)
				return
			}
			defer reader.Close()
			headers := map[string]string{
				"Content-Range":       fmt.Sprintf("bytes %d-%d/%d", rng.start, rng.end, size),
				"Accept-Ranges":       "bytes",
				"Content-Disposition": disposition,
			}
			c.DataFromReader(http.StatusPartialContent, rng.end-rng.start+1, contentType, reader, headers)
			return
		}
	}

	reader, err := h.bucket.DownloadFile(ctx, gcp.BucketCategoryMaterial, storageKey)
	if err != nil {
		h.log.Error("ViewPathNodeAsset failed (DownloadFile)", "error", err, "storage_key", storageKey)
		response.RespondError(c, http.StatusInternalServerError, "stream_failed", err)
		return
	}
	defer reader.Close()
	contentLength := size
	if contentLength <= 0 {
		contentLength = -1
	}
	headers := map[string]string{
		"Accept-Ranges":       "bytes",
		"Content-Disposition": disposition,
	}
	c.DataFromReader(http.StatusOK, contentLength, contentType, reader, headers)
}

func (h *PathHandler) rewriteNodeDocFigureAssetURLs(doc content.NodeDocV1, nodeID uuid.UUID) (content.NodeDocV1, bool) {
	if h == nil || h.bucket == nil || nodeID == uuid.Nil || len(doc.Blocks) == 0 {
		return doc, false
	}

	changed := false
	base := fmt.Sprintf("/api/path-nodes/%s/assets/view?key=", nodeID.String())

	for i := range doc.Blocks {
		b := doc.Blocks[i]
		if b == nil {
			continue
		}
		if strings.ToLower(strings.TrimSpace(stringFromAny(b["type"]))) != "figure" {
			continue
		}
		assetAny, ok := b["asset"]
		if !ok || assetAny == nil {
			continue
		}
		asset, ok := assetAny.(map[string]any)
		if !ok || asset == nil {
			continue
		}
		source := strings.ToLower(strings.TrimSpace(stringFromAny(asset["source"])))
		if source == "external" {
			continue
		}
		storageKey := strings.TrimSpace(stringFromAny(asset["storage_key"]))
		if storageKey == "" {
			continue
		}
		wantURL := base + url.QueryEscape(storageKey)
		if strings.TrimSpace(stringFromAny(asset["url"])) == wantURL {
			continue
		}
		asset["url"] = wantURL
		b["asset"] = asset
		changed = true
	}

	return doc, changed
}

type DocPatchSelection struct {
	Text  string `json:"text"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

type DocPatchRequest struct {
	BlockID        string             `json:"block_id"`
	BlockIndex     *int               `json:"block_index"`
	Action         string             `json:"action"`
	Instruction    string             `json:"instruction"`
	CitationPolicy string             `json:"citation_policy"`
	Selection      *DocPatchSelection `json:"selection"`
}

// POST /api/path-nodes/:id/doc/patch
func (h *PathHandler) EnqueuePathNodeDocPatch(c *gin.Context) {
	rd := ctxutil.GetRequestData(c.Request.Context())
	if rd == nil || rd.UserID == uuid.Nil {
		response.RespondError(c, http.StatusUnauthorized, "unauthorized", nil)
		return
	}
	if h.jobSvc == nil {
		response.RespondError(c, http.StatusInternalServerError, "job_service_missing", nil)
		return
	}

	nodeID, err := uuid.Parse(c.Param("id"))
	if err != nil || nodeID == uuid.Nil {
		response.RespondError(c, http.StatusBadRequest, "invalid_path_node_id", err)
		return
	}

	node, err := h.pathNodes.GetByID(dbctx.Context{Ctx: c.Request.Context()}, nodeID)
	if err != nil {
		h.log.Error("EnqueuePathNodeDocPatch failed (load node)", "error", err, "path_node_id", nodeID)
		response.RespondError(c, http.StatusInternalServerError, "load_node_failed", err)
		return
	}
	if node == nil || node.PathID == uuid.Nil {
		response.RespondError(c, http.StatusNotFound, "node_not_found", nil)
		return
	}

	pathRow, err := h.path.GetByID(dbctx.Context{Ctx: c.Request.Context()}, node.PathID)
	if err != nil {
		h.log.Error("EnqueuePathNodeDocPatch failed (load path)", "error", err, "path_id", node.PathID)
		response.RespondError(c, http.StatusInternalServerError, "load_path_failed", err)
		return
	}
	if pathRow == nil || pathRow.UserID == nil || *pathRow.UserID != rd.UserID {
		response.RespondError(c, http.StatusNotFound, "path_not_found", nil)
		return
	}

	docRow, err := h.nodeDocs.GetByPathNodeID(dbctx.Context{Ctx: c.Request.Context()}, nodeID)
	if err != nil {
		h.log.Error("EnqueuePathNodeDocPatch failed (load doc)", "error", err, "path_node_id", nodeID)
		response.RespondError(c, http.StatusInternalServerError, "load_doc_failed", err)
		return
	}
	if docRow == nil || len(docRow.DocJSON) == 0 || string(docRow.DocJSON) == "null" {
		response.RespondError(c, http.StatusNotFound, "doc_not_found", nil)
		return
	}

	var req DocPatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.RespondError(c, http.StatusBadRequest, "invalid_json", err)
		return
	}

	blockID := strings.TrimSpace(req.BlockID)
	blockIndex := -1
	if req.BlockIndex != nil {
		blockIndex = *req.BlockIndex
	}
	if blockID == "" && blockIndex < 0 {
		response.RespondError(c, http.StatusBadRequest, "missing_block_target", nil)
		return
	}

	action := strings.ToLower(strings.TrimSpace(req.Action))
	if action == "" {
		action = "rewrite"
	}
	if action != "rewrite" && action != "regen_media" {
		response.RespondError(c, http.StatusBadRequest, "invalid_action", nil)
		return
	}

	policy := strings.ToLower(strings.TrimSpace(req.CitationPolicy))
	if policy == "" {
		policy = "reuse_only"
	}
	if policy != "reuse_only" && policy != "allow_new" {
		response.RespondError(c, http.StatusBadRequest, "invalid_citation_policy", nil)
		return
	}

	payload := map[string]any{
		"path_node_id":    nodeID.String(),
		"action":          action,
		"instruction":     strings.TrimSpace(req.Instruction),
		"citation_policy": policy,
	}
	if blockID != "" {
		payload["block_id"] = blockID
	}
	if blockIndex >= 0 {
		payload["block_index"] = blockIndex
	}
	if req.Selection != nil {
		payload["selection"] = map[string]any{
			"text":  strings.TrimSpace(req.Selection.Text),
			"start": req.Selection.Start,
			"end":   req.Selection.End,
		}
	}

	entityID := nodeID
	job, err := h.jobSvc.Enqueue(dbctx.Context{Ctx: c.Request.Context()}, rd.UserID, "node_doc_patch", "path_node", &entityID, payload)
	if err != nil {
		h.log.Error("EnqueuePathNodeDocPatch failed (enqueue)", "error", err, "path_node_id", nodeID)
		response.RespondError(c, http.StatusInternalServerError, "enqueue_failed", err)
		return
	}

	response.RespondOK(c, gin.H{"job_id": job.ID})
}

// GET /api/path-nodes/:id/doc/revisions
func (h *PathHandler) ListPathNodeDocRevisions(c *gin.Context) {
	rd := ctxutil.GetRequestData(c.Request.Context())
	if rd == nil || rd.UserID == uuid.Nil {
		response.RespondError(c, http.StatusUnauthorized, "unauthorized", nil)
		return
	}
	if h.docRevisions == nil {
		response.RespondError(c, http.StatusInternalServerError, "revision_repo_missing", nil)
		return
	}

	nodeID, err := uuid.Parse(c.Param("id"))
	if err != nil || nodeID == uuid.Nil {
		response.RespondError(c, http.StatusBadRequest, "invalid_path_node_id", err)
		return
	}

	node, err := h.pathNodes.GetByID(dbctx.Context{Ctx: c.Request.Context()}, nodeID)
	if err != nil {
		h.log.Error("ListPathNodeDocRevisions failed (load node)", "error", err, "path_node_id", nodeID)
		response.RespondError(c, http.StatusInternalServerError, "load_node_failed", err)
		return
	}
	if node == nil || node.PathID == uuid.Nil {
		response.RespondError(c, http.StatusNotFound, "node_not_found", nil)
		return
	}

	pathRow, err := h.path.GetByID(dbctx.Context{Ctx: c.Request.Context()}, node.PathID)
	if err != nil {
		h.log.Error("ListPathNodeDocRevisions failed (load path)", "error", err, "path_id", node.PathID)
		response.RespondError(c, http.StatusInternalServerError, "load_path_failed", err)
		return
	}
	if pathRow == nil || pathRow.UserID == nil || *pathRow.UserID != rd.UserID {
		response.RespondError(c, http.StatusNotFound, "path_not_found", nil)
		return
	}

	limit := 0
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			limit = v
		}
	}
	includeDocs := strings.EqualFold(strings.TrimSpace(c.Query("include_docs")), "true") || c.Query("include_docs") == "1"

	rows, err := h.docRevisions.ListByPathNodeID(dbctx.Context{Ctx: c.Request.Context()}, nodeID, limit)
	if err != nil {
		h.log.Error("ListPathNodeDocRevisions failed (load revisions)", "error", err, "path_node_id", nodeID)
		response.RespondError(c, http.StatusInternalServerError, "load_revisions_failed", err)
		return
	}
	if !includeDocs {
		for _, r := range rows {
			if r != nil {
				r.BeforeJSON = nil
				r.AfterJSON = nil
			}
		}
	}

	response.RespondOK(c, gin.H{"revisions": rows})
}

// GET /api/path-nodes/:id/doc/materials
func (h *PathHandler) ListPathNodeDocMaterials(c *gin.Context) {
	rd := ctxutil.GetRequestData(c.Request.Context())
	if rd == nil || rd.UserID == uuid.Nil {
		response.RespondError(c, http.StatusUnauthorized, "unauthorized", nil)
		return
	}
	if h.materialFiles == nil || h.chunks == nil {
		response.RespondError(c, http.StatusInternalServerError, "material_repo_missing", nil)
		return
	}

	nodeID, err := uuid.Parse(c.Param("id"))
	if err != nil || nodeID == uuid.Nil {
		response.RespondError(c, http.StatusBadRequest, "invalid_path_node_id", err)
		return
	}

	node, err := h.pathNodes.GetByID(dbctx.Context{Ctx: c.Request.Context()}, nodeID)
	if err != nil {
		h.log.Error("ListPathNodeDocMaterials failed (load node)", "error", err, "path_node_id", nodeID)
		response.RespondError(c, http.StatusInternalServerError, "load_node_failed", err)
		return
	}
	if node == nil || node.PathID == uuid.Nil {
		response.RespondError(c, http.StatusNotFound, "node_not_found", nil)
		return
	}

	pathRow, err := h.path.GetByID(dbctx.Context{Ctx: c.Request.Context()}, node.PathID)
	if err != nil {
		h.log.Error("ListPathNodeDocMaterials failed (load path)", "error", err, "path_id", node.PathID)
		response.RespondError(c, http.StatusInternalServerError, "load_path_failed", err)
		return
	}
	if pathRow == nil || pathRow.UserID == nil || *pathRow.UserID != rd.UserID {
		response.RespondError(c, http.StatusNotFound, "path_not_found", nil)
		return
	}

	docRow, err := h.nodeDocs.GetByPathNodeID(dbctx.Context{Ctx: c.Request.Context()}, nodeID)
	if err != nil {
		h.log.Error("ListPathNodeDocMaterials failed (load doc)", "error", err, "path_node_id", nodeID)
		response.RespondError(c, http.StatusInternalServerError, "load_doc_failed", err)
		return
	}
	if docRow == nil || len(docRow.DocJSON) == 0 || string(docRow.DocJSON) == "null" {
		response.RespondError(c, http.StatusNotFound, "doc_not_found", nil)
		return
	}

	chunkIDs := dedupeUUIDsLocal(extractChunkIDsFromNodeDocJSON(docRow.DocJSON))
	if len(chunkIDs) == 0 {
		response.RespondOK(c, gin.H{"files": []any{}, "chunk_ids": []any{}, "chunk_ids_by_file": gin.H{}})
		return
	}

	chunks, err := h.chunks.GetByIDs(dbctx.Context{Ctx: c.Request.Context()}, chunkIDs)
	if err != nil {
		h.log.Error("ListPathNodeDocMaterials failed (load chunks)", "error", err, "path_node_id", nodeID)
		response.RespondError(c, http.StatusInternalServerError, "load_chunks_failed", err)
		return
	}

	fileIDSet := map[uuid.UUID]bool{}
	chunkIDsByFile := map[string][]string{}
	for _, ch := range chunks {
		if ch == nil || ch.MaterialFileID == uuid.Nil {
			continue
		}
		fileIDSet[ch.MaterialFileID] = true
		chunkIDsByFile[ch.MaterialFileID.String()] = append(chunkIDsByFile[ch.MaterialFileID.String()], ch.ID.String())
	}

	fileIDs := make([]uuid.UUID, 0, len(fileIDSet))
	for id := range fileIDSet {
		fileIDs = append(fileIDs, id)
	}

	files, err := h.materialFiles.GetByIDs(dbctx.Context{Ctx: c.Request.Context()}, fileIDs)
	if err != nil {
		h.log.Error("ListPathNodeDocMaterials failed (load files)", "error", err, "path_node_id", nodeID)
		response.RespondError(c, http.StatusInternalServerError, "load_files_failed", err)
		return
	}

	response.RespondOK(c, gin.H{
		"files":             files,
		"chunk_ids":         uuidStrings(chunkIDs),
		"chunk_ids_by_file": chunkIDsByFile,
	})
}

type DrillSpec struct {
	Kind           string `json:"kind"`
	Label          string `json:"label"`
	Reason         string `json:"reason,omitempty"`
	SuggestedCount int    `json:"suggested_count,omitempty"`
}

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

	node, err := h.pathNodes.GetByID(dbctx.Context{Ctx: c.Request.Context()}, nodeID)
	if err != nil {
		h.log.Error("ListPathNodeDrills failed (load node)", "error", err, "path_node_id", nodeID)
		response.RespondError(c, http.StatusInternalServerError, "load_node_failed", err)
		return
	}
	if node == nil || node.PathID == uuid.Nil {
		response.RespondError(c, http.StatusNotFound, "node_not_found", nil)
		return
	}

	pathRow, err := h.path.GetByID(dbctx.Context{Ctx: c.Request.Context()}, node.PathID)
	if err != nil {
		h.log.Error("ListPathNodeDrills failed (load path)", "error", err, "path_id", node.PathID)
		response.RespondError(c, http.StatusInternalServerError, "load_path_failed", err)
		return
	}
	if pathRow == nil || pathRow.UserID == nil || *pathRow.UserID != rd.UserID {
		response.RespondError(c, http.StatusNotFound, "path_not_found", nil)
		return
	}

	// v0: static recs (later: use node metadata + user profile + mastery)
	drills := []DrillSpec{
		{Kind: "flashcards", Label: "Flashcards", Reason: "Memorize key terms and definitions.", SuggestedCount: 12},
		{Kind: "quiz", Label: "Quick quiz", Reason: "Check understanding with grounded MCQs.", SuggestedCount: 8},
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
	kind := strings.ToLower(strings.TrimSpace(c.Param("kind")))
	if kind == "" {
		response.RespondError(c, http.StatusBadRequest, "missing_kind", nil)
		return
	}
	if kind != "quiz" && kind != "flashcards" {
		response.RespondError(c, http.StatusBadRequest, "unsupported_kind", fmt.Errorf("unsupported kind %q", kind))
		return
	}

	var req generateDrillRequest
	_ = c.ShouldBindJSON(&req)
	if req.Count <= 0 {
		req.Count = 0 // allow prompt defaults
	}

	node, err := h.pathNodes.GetByID(dbctx.Context{Ctx: c.Request.Context()}, nodeID)
	if err != nil {
		h.log.Error("GeneratePathNodeDrill failed (load node)", "error", err, "path_node_id", nodeID)
		response.RespondError(c, http.StatusInternalServerError, "load_node_failed", err)
		return
	}
	if node == nil || node.PathID == uuid.Nil {
		response.RespondError(c, http.StatusNotFound, "node_not_found", nil)
		return
	}

	pathRow, err := h.path.GetByID(dbctx.Context{Ctx: c.Request.Context()}, node.PathID)
	if err != nil {
		h.log.Error("GeneratePathNodeDrill failed (load path)", "error", err, "path_id", node.PathID)
		response.RespondError(c, http.StatusInternalServerError, "load_path_failed", err)
		return
	}
	if pathRow == nil || pathRow.UserID == nil || *pathRow.UserID != rd.UserID {
		response.RespondError(c, http.StatusNotFound, "path_not_found", nil)
		return
	}

	// Require that node content exists (drills are grounded in node content).
	docRow, _ := h.nodeDocs.GetByPathNodeID(dbctx.Context{Ctx: c.Request.Context()}, node.ID)

	switch kind {
	case "quiz":
		drill, err := h.generateDrillV1(c.Request.Context(), rd.UserID, node, docRow, kind, req.Count)
		if err != nil {
			response.RespondError(c, http.StatusInternalServerError, "generate_drill_failed", err)
			return
		}
		response.RespondOK(c, gin.H{"drill": drill})
		return

	case "flashcards":
		drill, err := h.generateDrillV1(c.Request.Context(), rd.UserID, node, docRow, kind, req.Count)
		if err != nil {
			response.RespondError(c, http.StatusInternalServerError, "generate_drill_failed", err)
			return
		}
		response.RespondOK(c, gin.H{"drill": drill})
		return

	default:
		response.RespondError(c, http.StatusBadRequest, "unsupported_kind", fmt.Errorf("unsupported kind %q", kind))
		return
	}
}

func (h *PathHandler) generateDrillV1(ctx context.Context, userID uuid.UUID, node *types.PathNode, docRow *types.LearningNodeDoc, kind string, count int) (any, error) {
	if h == nil || h.ai == nil || h.chunks == nil || h.drillInstances == nil {
		return nil, fmt.Errorf("drill generator not configured")
	}
	if node == nil || node.ID == uuid.Nil || node.PathID == uuid.Nil {
		return nil, fmt.Errorf("missing node")
	}
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind != "flashcards" && kind != "quiz" {
		return nil, fmt.Errorf("unsupported kind %q", kind)
	}

	// Defaults and bounds.
	switch kind {
	case "flashcards":
		if count <= 0 {
			count = 12
		}
		if count < 6 {
			count = 6
		}
		if count > 24 {
			count = 24
		}
	case "quiz":
		if count <= 0 {
			count = 8
		}
		if count < 4 {
			count = 4
		}
		if count > 12 {
			count = 12
		}
	}

	// Prefer doc-backed evidence; fallback to legacy node content markdown if doc missing.
	sourcesHash := ""
	var evidenceChunkIDs []uuid.UUID
	if docRow != nil && len(docRow.DocJSON) > 0 && string(docRow.DocJSON) != "null" {
		sourcesHash = strings.TrimSpace(docRow.SourcesHash)
		evidenceChunkIDs = extractChunkIDsFromNodeDocJSON(docRow.DocJSON)
	}

	if len(evidenceChunkIDs) == 0 {
		// Legacy fallback: derive evidence from node.ContentJSON citations.
		if len(node.ContentJSON) == 0 || string(node.ContentJSON) == "null" || strings.TrimSpace(string(node.ContentJSON)) == "" {
			return nil, fmt.Errorf("node content not ready")
		}
		_, citationsCSV := contentJSONToMarkdownAndCitations([]byte(node.ContentJSON))
		for _, s := range strings.Split(citationsCSV, ",") {
			if id, err := uuid.Parse(strings.TrimSpace(s)); err == nil && id != uuid.Nil {
				evidenceChunkIDs = append(evidenceChunkIDs, id)
			}
		}
		evidenceChunkIDs = dedupeUUIDsLocal(evidenceChunkIDs)
		sourcesHash = content.HashSources("legacy_node_content", 1, uuidStrings(evidenceChunkIDs))
	}

	if len(evidenceChunkIDs) == 0 {
		return nil, fmt.Errorf("no evidence chunks available")
	}

	if sourcesHash == "" {
		sourcesHash = content.HashSources("unknown_sources", 1, uuidStrings(evidenceChunkIDs))
	}

	// Cache lookup.
	if cached, err := h.drillInstances.GetByKey(dbctx.Context{Ctx: ctx}, userID, node.ID, kind, count, sourcesHash); err == nil && cached != nil && len(cached.PayloadJSON) > 0 && string(cached.PayloadJSON) != "null" {
		var obj any
		if json.Unmarshal(cached.PayloadJSON, &obj) == nil {
			return obj, nil
		}
	}

	// Load chunks and build excerpts.
	// Keep prompt small/deterministic.
	const maxEvidence = 18
	if len(evidenceChunkIDs) > maxEvidence {
		evidenceChunkIDs = evidenceChunkIDs[:maxEvidence]
	}
	chunks, err := h.chunks.GetByIDs(dbctx.Context{Ctx: ctx}, evidenceChunkIDs)
	if err != nil {
		return nil, err
	}
	chunkByID := map[uuid.UUID]*types.MaterialChunk{}
	allowed := map[string]bool{}
	for _, ch := range chunks {
		if ch == nil || ch.ID == uuid.Nil {
			continue
		}
		chunkByID[ch.ID] = ch
		allowed[ch.ID.String()] = true
	}

	excerpts := buildChunkExcerpts(chunkByID, evidenceChunkIDs, 16, 700)
	if strings.TrimSpace(excerpts) == "" {
		return nil, fmt.Errorf("empty evidence excerpts")
	}

	schemaMap, err := schema.DrillPayloadV1()
	if err != nil {
		return nil, err
	}

	system := `
You generate supplemental drills for studying, grounded ONLY in the provided excerpts.
Hard rules:
- Return ONLY valid JSON matching the schema.
- No learner-facing meta (no "Plan", no check-ins, no preference questions).
- Every item must include non-empty citations that reference ONLY provided chunk_ids.
`

	var user string
	switch kind {
	case "flashcards":
		user = fmt.Sprintf(`
DRILL_KIND: flashcards
TARGET_CARD_COUNT: %d
NODE_TITLE: %s

GROUNDING_EXCERPTS (chunk_id lines):
%s

Task:
- Output kind="flashcards"
- Produce exactly TARGET_CARD_COUNT cards (or as close as possible if constrained by excerpts).
- Cards should be atomic and test a single idea.
- Keep fronts short; backs can be 1-4 sentences.
- citations must reference provided chunk_ids only.
- Set questions=[].
Return JSON only.`, count, node.Title, excerpts)
	case "quiz":
		user = fmt.Sprintf(`
DRILL_KIND: quiz
TARGET_QUESTION_COUNT: %d
NODE_TITLE: %s

GROUNDING_EXCERPTS (chunk_id lines):
%s

Task:
- Output kind="quiz"
- Produce exactly TARGET_QUESTION_COUNT MCQs.
- Each question must have 4 options with stable ids like "a","b","c","d".
- answer_id must match one of the option ids.
- explanation_md should justify using the excerpts (no new facts).
- citations must reference provided chunk_ids only.
- Set cards=[].
Return JSON only.`, count, node.Title, excerpts)
	}

	var lastErrs []string
	for attempt := 1; attempt <= 3; attempt++ {
		feedback := ""
		if len(lastErrs) > 0 {
			feedback = "\n\nVALIDATION_ERRORS_TO_FIX:\n- " + strings.Join(lastErrs, "\n- ")
		}

		start := time.Now()
		obj, genErr := h.ai.GenerateJSON(ctx, system, user+feedback, "drill_payload_v1", schemaMap)
		latency := int(time.Since(start).Milliseconds())
		if genErr != nil {
			lastErrs = []string{"generate_failed: " + genErr.Error()}
			h.recordGenRun(ctx, "drill", nil, userID, node.PathID, node.ID, "failed", "drill_v1@1:"+kind, attempt, latency, lastErrs, nil)
			continue
		}

		raw, _ := json.Marshal(obj)
		var payload content.DrillPayloadV1
		if err := json.Unmarshal(raw, &payload); err != nil {
			lastErrs = []string{"schema_unmarshal_failed"}
			h.recordGenRun(ctx, "drill", nil, userID, node.PathID, node.ID, "failed", "drill_v1@1:"+kind, attempt, latency, lastErrs, nil)
			continue
		}

		// Best-effort scrub for occasional meta phrasing that can slip into learner-facing drill text.
		if scrubbed, phrases := content.ScrubDrillPayloadV1(payload); len(phrases) > 0 {
			payload = scrubbed
		}

		minCount := count
		maxCount := count
		if kind == "flashcards" {
			minCount = count - 2
			maxCount = count + 2
			if minCount < 6 {
				minCount = 6
			}
			if maxCount > 24 {
				maxCount = 24
			}
		} else if kind == "quiz" {
			minCount = count
			maxCount = count
		}

		errs, qm := content.ValidateDrillPayloadV1(payload, allowed, kind, minCount, maxCount)
		if len(errs) > 0 {
			lastErrs = errs
			h.recordGenRun(ctx, "drill", nil, userID, node.PathID, node.ID, "failed", "drill_v1@1:"+kind, attempt, latency, errs, qm)
			continue
		}

		// Persist the scrubbed-and-validated payload (not the raw model output bytes).
		raw, _ = json.Marshal(payload)
		canon, err := content.CanonicalizeJSON(raw)
		if err != nil {
			return nil, err
		}
		contentHash := content.HashBytes(canon)

		row := &types.LearningDrillInstance{
			ID:            uuid.New(),
			UserID:        userID,
			PathID:        node.PathID,
			PathNodeID:    node.ID,
			Kind:          kind,
			Count:         count,
			SourcesHash:   sourcesHash,
			SchemaVersion: 1,
			PayloadJSON:   datatypes.JSON(canon),
			ContentHash:   contentHash,
			CreatedAt:     time.Now().UTC(),
			UpdatedAt:     time.Now().UTC(),
		}
		_ = h.drillInstances.Upsert(dbctx.Context{Ctx: ctx}, row)
		h.recordGenRun(ctx, "drill", &row.ID, userID, node.PathID, node.ID, "succeeded", "drill_v1@1:"+kind, attempt, latency, nil, map[string]any{
			"content_hash": contentHash,
			"sources_hash": sourcesHash,
		})

		var out any
		_ = json.Unmarshal(canon, &out)
		return out, nil
	}

	return nil, fmt.Errorf("drill generation failed after retries: %v", lastErrs)
}

func (h *PathHandler) recordGenRun(ctx context.Context, artifactType string, artifactID *uuid.UUID, userID uuid.UUID, pathID uuid.UUID, pathNodeID uuid.UUID, status string, promptVersion string, attempt int, latencyMS int, validationErrors []string, qualityMetrics map[string]any) {
	if h == nil || h.genRuns == nil {
		return
	}
	ve := datatypes.JSON([]byte(`null`))
	if len(validationErrors) > 0 {
		b, _ := json.Marshal(validationErrors)
		ve = datatypes.JSON(b)
	}
	qm := datatypes.JSON([]byte(`null`))
	if qualityMetrics != nil {
		b, _ := json.Marshal(qualityMetrics)
		qm = datatypes.JSON(b)
	}
	model := strings.TrimSpace(os.Getenv("OPENAI_MODEL"))
	if model == "" {
		model = "unknown"
	}
	_, _ = h.genRuns.Create(dbctx.Context{Ctx: ctx}, []*types.LearningDocGenerationRun{{
		ID:               uuid.New(),
		ArtifactType:     artifactType,
		ArtifactID:       artifactID,
		UserID:           userID,
		PathID:           pathID,
		PathNodeID:       pathNodeID,
		Status:           status,
		Model:            model,
		PromptVersion:    promptVersion,
		Attempt:          attempt,
		LatencyMS:        latencyMS,
		TokensIn:         0,
		TokensOut:        0,
		ValidationErrors: ve,
		QualityMetrics:   qm,
		CreatedAt:        time.Now().UTC(),
	}})
}

func buildChunkExcerpts(byID map[uuid.UUID]*types.MaterialChunk, ids []uuid.UUID, maxLines int, maxChars int) string {
	if maxLines <= 0 {
		maxLines = 12
	}
	if maxChars <= 0 {
		maxChars = 700
	}
	var b strings.Builder
	n := 0
	seen := map[uuid.UUID]bool{}
	for _, id := range ids {
		if id == uuid.Nil || seen[id] {
			continue
		}
		seen[id] = true
		ch := byID[id]
		if ch == nil {
			continue
		}
		txt := strings.TrimSpace(ch.Text)
		if txt == "" {
			continue
		}
		if len(txt) > maxChars {
			txt = txt[:maxChars] + "..."
		}
		b.WriteString("[chunk_id=")
		b.WriteString(id.String())
		b.WriteString("] ")
		b.WriteString(txt)
		b.WriteString("\n")
		n++
		if n >= maxLines {
			break
		}
	}
	return strings.TrimSpace(b.String())
}

func extractChunkIDsFromNodeDocJSON(raw datatypes.JSON) []uuid.UUID {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var obj map[string]any
	if json.Unmarshal(raw, &obj) != nil {
		return nil
	}
	blocks, _ := obj["blocks"].([]any)
	out := make([]uuid.UUID, 0)
	seen := map[uuid.UUID]bool{}
	for _, b := range blocks {
		m, ok := b.(map[string]any)
		if !ok {
			continue
		}
		for _, c := range stringSliceFromAny(extractChunkIDsFromCitations(m["citations"])) {
			if id, err := uuid.Parse(strings.TrimSpace(c)); err == nil && id != uuid.Nil && !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	return out
}

func extractChunkIDsFromCitations(raw any) []string {
	arr, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, it := range arr {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		id := strings.TrimSpace(stringFromAny(m["chunk_id"]))
		if id != "" {
			out = append(out, id)
		}
	}
	return out
}

func dedupeUUIDsLocal(in []uuid.UUID) []uuid.UUID {
	seen := map[uuid.UUID]bool{}
	out := make([]uuid.UUID, 0, len(in))
	for _, id := range in {
		if id == uuid.Nil || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func uuidStrings(in []uuid.UUID) []string {
	out := make([]string, 0, len(in))
	for _, id := range in {
		if id != uuid.Nil {
			out = append(out, id.String())
		}
	}
	return out
}

func contentJSONToMarkdownAndCitations(raw []byte) (string, string) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", ""
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return "", ""
	}

	// citations is optional in ContentJSONSchema but is expected in generated outputs.
	citations := []string{}
	if v, ok := obj["citations"]; ok {
		citations = append(citations, stringSliceFromAny(v)...)
	}

	blocksAny, _ := obj["blocks"].([]any)
	var b strings.Builder
	for _, rawBlock := range blocksAny {
		m, ok := rawBlock.(map[string]any)
		if !ok || m == nil {
			continue
		}
		kind := strings.ToLower(strings.TrimSpace(stringFromAny(m["kind"])))
		content := strings.TrimSpace(stringFromAny(m["content_md"]))
		items := stringSliceFromAny(m["items"])
		assetRefs := stringSliceFromAny(m["asset_refs"])

		switch kind {
		case "heading":
			if content != "" {
				b.WriteString("## ")
				b.WriteString(content)
				b.WriteString("\n\n")
			}
		case "paragraph", "callout":
			if content != "" {
				b.WriteString(content)
				b.WriteString("\n\n")
			}
		case "bullets":
			for _, it := range items {
				it = strings.TrimSpace(it)
				if it == "" {
					continue
				}
				b.WriteString("- ")
				b.WriteString(it)
				b.WriteString("\n")
			}
			if len(items) > 0 {
				b.WriteString("\n")
			}
		case "steps":
			n := 0
			for _, it := range items {
				it = strings.TrimSpace(it)
				if it == "" {
					continue
				}
				n++
				b.WriteString(fmt.Sprintf("%d. %s\n", n, it))
			}
			if n > 0 {
				b.WriteString("\n")
			}
		case "divider":
			b.WriteString("\n---\n\n")
		case "image":
			if len(assetRefs) > 0 {
				b.WriteString(fmt.Sprintf("[image: %s]\n\n", assetRefs[0]))
			}
		case "video_embed":
			if len(assetRefs) > 0 {
				b.WriteString(fmt.Sprintf("[video: %s]\n\n", assetRefs[0]))
			}
		default:
			if content != "" {
				b.WriteString(content)
				b.WriteString("\n\n")
			}
		}
	}

	md := strings.TrimSpace(b.String())
	csv := strings.Join(dedupeStrings(citations), ", ")
	return md, csv
}

func stringFromAny(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	default:
		return fmt.Sprint(v)
	}
}

func stringSliceFromAny(v any) []string {
	if v == nil {
		return nil
	}
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, it := range t {
			s := strings.TrimSpace(stringFromAny(it))
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func dedupeStrings(in []string) []string {
	if len(in) == 0 {
		return in
	}
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
