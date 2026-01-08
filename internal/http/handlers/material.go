package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yungbote/neurobridge-backend/internal/clients/gcp"
	"github.com/yungbote/neurobridge-backend/internal/data/repos"
	"github.com/yungbote/neurobridge-backend/internal/http/response"
	"github.com/yungbote/neurobridge-backend/internal/pkg/ctxutil"
	"github.com/yungbote/neurobridge-backend/internal/pkg/dbctx"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"github.com/yungbote/neurobridge-backend/internal/realtime"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

func assetPrimaryMimeType(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return ""
	}
	v, _ := obj["mime"]
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

type MaterialHandler struct {
	log      *logger.Logger
	workflow services.WorkflowService
	sseHub   *realtime.SSEHub

	bucket           gcp.BucketService
	materialFiles    repos.MaterialFileRepo
	materialAssets   repos.MaterialAssetRepo
	userLibraryIndex repos.UserLibraryIndexRepo
}

func NewMaterialHandler(
	log *logger.Logger,
	workflow services.WorkflowService,
	sseHub *realtime.SSEHub,
	bucket gcp.BucketService,
	materialFiles repos.MaterialFileRepo,
	materialAssets repos.MaterialAssetRepo,
	userLibraryIndex repos.UserLibraryIndexRepo,
) *MaterialHandler {
	return &MaterialHandler{
		log:              log.With("handler", "MaterialHandler"),
		workflow:         workflow,
		sseHub:           sseHub,
		bucket:           bucket,
		materialFiles:    materialFiles,
		materialAssets:   materialAssets,
		userLibraryIndex: userLibraryIndex,
	}
}

func (h *MaterialHandler) UploadMaterials(c *gin.Context) {
	rd := ctxutil.GetRequestData(c.Request.Context())
	if rd == nil || rd.UserID == uuid.Nil {
		response.RespondError(c, http.StatusUnauthorized, "unauthorized", nil)
		return
	}
	userID := rd.UserID
	if err := c.Request.ParseMultipartForm(32 << 20); err != nil {
		response.RespondError(c, http.StatusBadRequest, "invalid_multipart_form", err)
		return
	}
	form := c.Request.MultipartForm
	prompt := ""
	if form != nil {
		if v := form.Value["prompt"]; len(v) > 0 {
			prompt = strings.TrimSpace(v[0])
		}
		if prompt == "" {
			if v := form.Value["message"]; len(v) > 0 {
				prompt = strings.TrimSpace(v[0])
			}
		}
	}
	if len(prompt) > 20000 {
		response.RespondError(c, http.StatusBadRequest, "prompt_too_large", nil)
		return
	}
	fileHeaders := form.File["files"]
	if len(fileHeaders) == 0 && strings.TrimSpace(prompt) == "" {
		response.RespondError(c, http.StatusBadRequest, "no_files_or_prompt", nil)
		return
	}
	uploaded := make([]services.UploadedFileInfo, 0, len(fileHeaders))
	defer func() {
		for _, uf := range uploaded {
			if rc, ok := uf.Reader.(io.ReadCloser); ok {
				_ = rc.Close()
			}
		}
	}()
	for _, fh := range fileHeaders {
		mimeHeader := fh.Header.Get("Content-Type")
		// Sniff
		sniffFile, err := fh.Open()
		if err != nil {
			h.log.Error("cannot open file for sniffing", "error", err)
			continue
		}
		buf := make([]byte, 512)
		n, _ := sniffFile.Read(buf)
		_ = sniffFile.Close()
		sniffType := http.DetectContentType(buf[:n])
		mimeType := mimeHeader
		if mimeType == "" {
			mimeType = sniffType
		}
		r, err := fh.Open()
		if err != nil {
			h.log.Error("cannot open file for reading", "error", err)
			continue
		}
		uploaded = append(uploaded, services.UploadedFileInfo{
			OriginalName: fh.Filename,
			MimeType:     mimeType,
			SizeBytes:    fh.Size,
			Reader:       r,
		})
	}
	if len(uploaded) == 0 && strings.TrimSpace(prompt) == "" {
		response.RespondError(c, http.StatusBadRequest, "could_not_read_files", nil)
		return
	}
	dbc := dbctx.Context{Ctx: c.Request.Context()}
	set, pathID, thread, job, err := h.workflow.UploadMaterialsAndStartLearningBuildWithChat(dbc, userID, uploaded, prompt)
	if err != nil {
		response.RespondError(c, http.StatusInternalServerError, "workflow_failed", err)
		return
	}
	// Flush request-scoped SSE messages if any (we still keep this pattern).
	ssd := ctxutil.GetSSEData(c.Request.Context())
	if ssd != nil && len(ssd.Messages) > 0 {
		for _, msg := range ssd.Messages {
			h.sseHub.Broadcast(msg)
		}
		ssd.Messages = nil
	}

	response.RespondOK(c, gin.H{
		"ok":              true,
		"material_set_id": set.ID,
		"path_id":         pathID,
		"thread_id":       thread.ID,
		"job_id":          job.ID,
	})
}

// GET /api/material-files
func (h *MaterialHandler) ListUserMaterialFiles(c *gin.Context) {
	rd := ctxutil.GetRequestData(c.Request.Context())
	if rd == nil || rd.UserID == uuid.Nil {
		response.RespondError(c, http.StatusUnauthorized, "unauthorized", nil)
		return
	}
	if h.materialFiles == nil || h.userLibraryIndex == nil {
		response.RespondError(c, http.StatusInternalServerError, "material_repo_missing", nil)
		return
	}

	idxRows, err := h.userLibraryIndex.GetByUserIDs(dbctx.Context{Ctx: c.Request.Context()}, []uuid.UUID{rd.UserID})
	if err != nil {
		h.log.Error("ListUserMaterialFiles failed (load library index)", "error", err, "user_id", rd.UserID)
		response.RespondError(c, http.StatusInternalServerError, "load_library_index_failed", err)
		return
	}
	if len(idxRows) == 0 {
		response.RespondOK(c, gin.H{"files": []any{}})
		return
	}

	setIDs := make([]uuid.UUID, 0, len(idxRows))
	seen := map[uuid.UUID]struct{}{}
	for _, row := range idxRows {
		if row == nil || row.MaterialSetID == uuid.Nil {
			continue
		}
		if _, ok := seen[row.MaterialSetID]; ok {
			continue
		}
		seen[row.MaterialSetID] = struct{}{}
		setIDs = append(setIDs, row.MaterialSetID)
	}
	if len(setIDs) == 0 {
		response.RespondOK(c, gin.H{"files": []any{}})
		return
	}

	files, err := h.materialFiles.GetByMaterialSetIDs(dbctx.Context{Ctx: c.Request.Context()}, setIDs)
	if err != nil {
		h.log.Error("ListUserMaterialFiles failed (load files)", "error", err, "user_id", rd.UserID)
		response.RespondError(c, http.StatusInternalServerError, "load_files_failed", err)
		return
	}

	response.RespondOK(c, gin.H{"files": files})
}

type byteRange struct {
	start int64
	end   int64
}

func parseByteRangeHeader(rangeHeader string, size int64) (byteRange, bool, error) {
	rh := strings.TrimSpace(rangeHeader)
	if rh == "" {
		return byteRange{}, false, nil
	}
	if size <= 0 {
		return byteRange{}, false, fmt.Errorf("unknown object size")
	}
	if !strings.HasPrefix(rh, "bytes=") {
		return byteRange{}, false, fmt.Errorf("unsupported range unit")
	}
	parts := strings.Split(strings.TrimPrefix(rh, "bytes="), ",")
	if len(parts) != 1 {
		return byteRange{}, false, fmt.Errorf("multiple ranges not supported")
	}
	part := strings.TrimSpace(parts[0])
	if part == "" {
		return byteRange{}, false, fmt.Errorf("empty range")
	}
	if strings.HasPrefix(part, "-") {
		suffix := strings.TrimPrefix(part, "-")
		n, err := strconv.ParseInt(suffix, 10, 64)
		if err != nil || n <= 0 {
			return byteRange{}, false, fmt.Errorf("invalid suffix range")
		}
		if n > size {
			n = size
		}
		return byteRange{start: size - n, end: size - 1}, true, nil
	}

	bounds := strings.Split(part, "-")
	if len(bounds) != 2 {
		return byteRange{}, false, fmt.Errorf("invalid range format")
	}
	start, err := strconv.ParseInt(bounds[0], 10, 64)
	if err != nil || start < 0 {
		return byteRange{}, false, fmt.Errorf("invalid range start")
	}
	var end int64
	if bounds[1] == "" {
		end = size - 1
	} else {
		end, err = strconv.ParseInt(bounds[1], 10, 64)
		if err != nil || end < 0 {
			return byteRange{}, false, fmt.Errorf("invalid range end")
		}
	}
	if start >= size || end < start {
		return byteRange{}, false, fmt.Errorf("range out of bounds")
	}
	if end >= size {
		end = size - 1
	}
	return byteRange{start: start, end: end}, true, nil
}

func sanitizeFilename(name string) string {
	base := filepath.Base(strings.TrimSpace(name))
	if base == "." || base == "/" {
		return ""
	}
	base = strings.ReplaceAll(base, "\"", "")
	base = strings.ReplaceAll(base, "\\", "")
	return strings.TrimSpace(base)
}

func resolveContentType(primary, bucketType, filename, storageKey string) string {
	for _, v := range []string{
		strings.TrimSpace(primary),
		strings.TrimSpace(bucketType),
		strings.TrimSpace(mime.TypeByExtension(filepath.Ext(filename))),
		strings.TrimSpace(mime.TypeByExtension(filepath.Ext(storageKey))),
	} {
		if v != "" {
			return v
		}
	}
	return "application/octet-stream"
}

func buildContentDisposition(filename string, download bool) string {
	disposition := "inline"
	if download {
		disposition = "attachment"
	}
	name := sanitizeFilename(filename)
	if name == "" {
		return disposition
	}
	return fmt.Sprintf("%s; filename=\"%s\"", disposition, name)
}

func (h *MaterialHandler) streamMaterialObject(
	c *gin.Context,
	storageKey string,
	filename string,
	mimeType string,
) {
	if h.bucket == nil {
		response.RespondError(c, http.StatusInternalServerError, "bucket_unavailable", nil)
		return
	}
	if strings.TrimSpace(storageKey) == "" {
		response.RespondError(c, http.StatusNotFound, "material_not_found", nil)
		return
	}

	ctx := c.Request.Context()
	attrs, err := h.bucket.GetObjectAttrs(ctx, gcp.BucketCategoryMaterial, storageKey)
	if err != nil {
		h.log.Error("GetObjectAttrs failed", "error", err, "storage_key", storageKey)
		response.RespondError(c, http.StatusNotFound, "material_not_found", err)
		return
	}

	contentType := resolveContentType(mimeType, attrs.ContentType, filename, storageKey)
	disposition := buildContentDisposition(filename, c.Query("download") != "")
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
				h.log.Error("OpenRangeReader failed", "error", err, "storage_key", storageKey)
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
		h.log.Error("DownloadFile failed", "error", err, "storage_key", storageKey)
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

// GET /api/material-files/:id/view
func (h *MaterialHandler) ViewMaterialFile(c *gin.Context) {
	rd := ctxutil.GetRequestData(c.Request.Context())
	if rd == nil || rd.UserID == uuid.Nil {
		response.RespondError(c, http.StatusUnauthorized, "unauthorized", nil)
		return
	}
	if h.materialFiles == nil || h.userLibraryIndex == nil {
		response.RespondError(c, http.StatusInternalServerError, "material_repo_missing", nil)
		return
	}

	fileID, err := uuid.Parse(c.Param("id"))
	if err != nil || fileID == uuid.Nil {
		response.RespondError(c, http.StatusBadRequest, "invalid_material_file_id", err)
		return
	}

	files, err := h.materialFiles.GetByIDs(dbctx.Context{Ctx: c.Request.Context()}, []uuid.UUID{fileID})
	if err != nil || len(files) == 0 || files[0] == nil {
		response.RespondError(c, http.StatusNotFound, "material_not_found", err)
		return
	}
	file := files[0]

	idx, err := h.userLibraryIndex.GetByUserAndMaterialSet(dbctx.Context{Ctx: c.Request.Context()}, rd.UserID, file.MaterialSetID)
	if err != nil {
		response.RespondError(c, http.StatusInternalServerError, "load_library_index_failed", err)
		return
	}
	if idx == nil {
		response.RespondError(c, http.StatusNotFound, "material_not_found", nil)
		return
	}

	h.streamMaterialObject(c, file.StorageKey, file.OriginalName, file.MimeType)
}

// GET /api/material-files/:id/thumbnail
func (h *MaterialHandler) ViewMaterialFileThumbnail(c *gin.Context) {
	rd := ctxutil.GetRequestData(c.Request.Context())
	if rd == nil || rd.UserID == uuid.Nil {
		response.RespondError(c, http.StatusUnauthorized, "unauthorized", nil)
		return
	}
	if h.materialFiles == nil || h.userLibraryIndex == nil {
		response.RespondError(c, http.StatusInternalServerError, "material_repo_missing", nil)
		return
	}

	fileID, err := uuid.Parse(c.Param("id"))
	if err != nil || fileID == uuid.Nil {
		response.RespondError(c, http.StatusBadRequest, "invalid_material_file_id", err)
		return
	}

	files, err := h.materialFiles.GetByIDs(dbctx.Context{Ctx: c.Request.Context()}, []uuid.UUID{fileID})
	if err != nil || len(files) == 0 || files[0] == nil {
		response.RespondError(c, http.StatusNotFound, "material_not_found", err)
		return
	}
	file := files[0]

	idx, err := h.userLibraryIndex.GetByUserAndMaterialSet(dbctx.Context{Ctx: c.Request.Context()}, rd.UserID, file.MaterialSetID)
	if err != nil {
		response.RespondError(c, http.StatusInternalServerError, "load_library_index_failed", err)
		return
	}
	if idx == nil {
		response.RespondError(c, http.StatusNotFound, "material_not_found", nil)
		return
	}

	if file.ThumbnailAssetID != nil && *file.ThumbnailAssetID != uuid.Nil && h.materialAssets != nil {
		asset, err := h.materialAssets.GetByID(dbctx.Context{Ctx: c.Request.Context()}, *file.ThumbnailAssetID)
		if err == nil && asset != nil && asset.MaterialFileID == file.ID && strings.TrimSpace(asset.StorageKey) != "" {
			filename := fmt.Sprintf("%s%s", asset.ID.String(), filepath.Ext(asset.StorageKey))
			h.streamMaterialObject(c, asset.StorageKey, filename, assetPrimaryMimeType(asset.Metadata))
			return
		}
	}

	// Fallback: if the original upload is already an image, use it.
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(file.MimeType)), "image/") {
		h.streamMaterialObject(c, file.StorageKey, file.OriginalName, file.MimeType)
		return
	}

	response.RespondError(c, http.StatusNotFound, "thumbnail_not_found", nil)
}

// GET /api/material-assets/:id/view
func (h *MaterialHandler) ViewMaterialAsset(c *gin.Context) {
	rd := ctxutil.GetRequestData(c.Request.Context())
	if rd == nil || rd.UserID == uuid.Nil {
		response.RespondError(c, http.StatusUnauthorized, "unauthorized", nil)
		return
	}
	if h.materialAssets == nil || h.materialFiles == nil || h.userLibraryIndex == nil {
		response.RespondError(c, http.StatusInternalServerError, "material_repo_missing", nil)
		return
	}

	assetID, err := uuid.Parse(c.Param("id"))
	if err != nil || assetID == uuid.Nil {
		response.RespondError(c, http.StatusBadRequest, "invalid_material_asset_id", err)
		return
	}

	asset, err := h.materialAssets.GetByID(dbctx.Context{Ctx: c.Request.Context()}, assetID)
	if err != nil || asset == nil || asset.ID == uuid.Nil {
		response.RespondError(c, http.StatusNotFound, "material_not_found", err)
		return
	}
	if asset.MaterialFileID == uuid.Nil {
		response.RespondError(c, http.StatusNotFound, "material_not_found", nil)
		return
	}

	files, err := h.materialFiles.GetByIDs(dbctx.Context{Ctx: c.Request.Context()}, []uuid.UUID{asset.MaterialFileID})
	if err != nil || len(files) == 0 || files[0] == nil {
		response.RespondError(c, http.StatusNotFound, "material_not_found", err)
		return
	}
	file := files[0]
	idx, err := h.userLibraryIndex.GetByUserAndMaterialSet(dbctx.Context{Ctx: c.Request.Context()}, rd.UserID, file.MaterialSetID)
	if err != nil {
		response.RespondError(c, http.StatusInternalServerError, "load_library_index_failed", err)
		return
	}
	if idx == nil {
		response.RespondError(c, http.StatusNotFound, "material_not_found", nil)
		return
	}

	filename := fmt.Sprintf("%s%s", asset.ID.String(), filepath.Ext(asset.StorageKey))
	h.streamMaterialObject(c, asset.StorageKey, filename, assetPrimaryMimeType(asset.Metadata))
}
