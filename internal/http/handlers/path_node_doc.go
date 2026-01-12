package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/datatypes"

	types "github.com/yungbote/neurobridge-backend/internal/domain"
	"github.com/yungbote/neurobridge-backend/internal/http/response"
	"github.com/yungbote/neurobridge-backend/internal/modules/learning/content"
	"github.com/yungbote/neurobridge-backend/internal/platform/ctxutil"
	"github.com/yungbote/neurobridge-backend/internal/platform/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/platform/gcp"
)

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
