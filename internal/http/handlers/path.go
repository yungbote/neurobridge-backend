package handlers

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/http/response"
	learningmod "github.com/yungbote/neurobridge-backend/internal/modules/learning"
	"github.com/yungbote/neurobridge-backend/internal/platform/ctxutil"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/envutil"
	"github.com/yungbote/neurobridge-backend/internal/platform/gcp"
)

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

	// Default UX: hide archived paths unless explicitly requested.
	includeArchived := false
	if v := strings.ToLower(strings.TrimSpace(c.Query("include_archived"))); v != "" {
		includeArchived = v == "1" || v == "true" || v == "yes" || v == "y"
	}
	if !includeArchived && len(paths) > 0 {
		filtered := make([]*types.Path, 0, len(paths))
		for _, p := range paths {
			if p == nil {
				continue
			}
			if strings.EqualFold(strings.TrimSpace(p.Status), "archived") {
				continue
			}
			filtered = append(filtered, p)
		}
		paths = filtered
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
			// Back-compat: older installs derived material_set_id from user_library_index.
			// Newer installs store it directly on the path row.
			if row.MaterialSetID == nil || *row.MaterialSetID == uuid.Nil {
				msid := idx.MaterialSetID
				row.MaterialSetID = &msid
			}
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
			if row.MaterialSetID == nil || *row.MaterialSetID == uuid.Nil {
				msid := idx.MaterialSetID
				row.MaterialSetID = &msid
			}
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

	out, err := h.learning.WithLog(h.log).PathCoverRender(c.Request.Context(), learningmod.PathCoverRenderInput{
		PathID: pathID,
		Force:  force,
	})
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

	// Resolve material_set_id for this path:
	// - prefer the value stored on the path row (supports subpaths),
	// - fall back to user_library_index for older installs / root paths.
	materialSetID := uuid.Nil
	if pathRow.MaterialSetID != nil && *pathRow.MaterialSetID != uuid.Nil {
		materialSetID = *pathRow.MaterialSetID
	} else {
		idxRow, err := h.userLibraryIndex.GetByUserAndPathID(dbctx.Context{Ctx: c.Request.Context()}, rd.UserID, pathID)
		if err != nil {
			h.log.Error("ListPathMaterials failed (load library index)", "error", err, "path_id", pathID)
			response.RespondError(c, http.StatusInternalServerError, "load_library_index_failed", err)
			return
		}
		if idxRow != nil && idxRow.MaterialSetID != uuid.Nil {
			materialSetID = idxRow.MaterialSetID
			// Backfill on the path row for future calls (best-effort).
			_ = h.path.UpdateFields(dbctx.Context{Ctx: c.Request.Context()}, pathID, map[string]interface{}{"material_set_id": materialSetID})
		}
	}
	if materialSetID == uuid.Nil {
		response.RespondOK(c, gin.H{"files": []any{}, "assets": []any{}, "assets_by_file": gin.H{}})
		return
	}

	files, err := h.materialFiles.GetByMaterialSetID(dbctx.Context{Ctx: c.Request.Context()}, materialSetID)
	if err != nil {
		h.log.Error("ListPathMaterials failed (load files)", "error", err, "material_set_id", materialSetID)
		response.RespondError(c, http.StatusInternalServerError, "load_files_failed", err)
		return
	}

	// If this path has an intake material filter allowlist (e.g., subpaths), apply it.
	if allow := materialFileAllowlistFromPathMetaJSON(pathRow.Metadata); len(allow) > 0 {
		files = filterMaterialFilesByAllowlist(files, allow)
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
			h.log.Error("ListPathMaterials failed (load assets)", "error", err, "material_set_id", materialSetID)
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
		"material_set_id": materialSetID.String(),
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
