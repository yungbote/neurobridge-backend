package handlers

import (
	"io"
	"net/http"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/logger"
	"github.com/yungbote/neurobridge-backend/internal/requestdata"
	"github.com/yungbote/neurobridge-backend/internal/services"
	"github.com/yungbote/neurobridge-backend/internal/ssedata"
	"github.com/yungbote/neurobridge-backend/internal/sse"
	"github.com/yungbote/neurobridge-backend/internal/http/response"
)

type MaterialHandler struct {
	log      *logger.Logger
	workflow services.WorkflowService
	sseHub   *sse.SSEHub
}

func NewMaterialHandler(log *logger.Logger, workflow services.WorkflowService, sseHub *sse.SSEHub) *MaterialHandler {
	return &MaterialHandler{
		log:      log.With("handler", "MaterialHandler"),
		workflow: workflow,
		sseHub:   sseHub,
	}
}

func (h *MaterialHandler) UploadMaterials(c *gin.Context) {
	rd := requestdata.GetRequestData(c.Request.Context())
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
	fileHeaders := form.File["files"]
	if len(fileHeaders) == 0 {
		response.RespondError(c, http.StatusBadRequest, "no_files", nil)
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
	if len(uploaded) == 0 {
		response.RespondError(c, http.StatusBadRequest, "could_not_read_files", nil)
		return
	}
	set, course, job, err := h.workflow.UploadMaterialsAndStartCourseBuild(c.Request.Context(), nil, userID, uploaded)
	if err != nil {
		response.RespondError(c, http.StatusInternalServerError, "workflow_failed", err)
		return
	}
	// Flush request-scoped SSE messages if any (we still keep this pattern).
	ssd := ssedata.GetSSEData(c.Request.Context())
	if ssd != nil && len(ssd.Messages) > 0 {
		for _, msg := range ssd.Messages {
			h.sseHub.Broadcast(msg)
		}
		ssd.Messages = nil
	}

	response.RespondOK(c, gin.H{
		"ok":              true,
		"material_set_id": set.ID,
		"course_id":       course.ID,
		"job_id":          job.ID,
	})
}










