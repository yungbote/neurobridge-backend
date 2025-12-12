package handlers

import (
  "io"
  "net/http"

  "github.com/gin-gonic/gin"
  "github.com/google/uuid"

  "github.com/yungbote/neurobridge-backend/internal/requestdata"
  "github.com/yungbote/neurobridge-backend/internal/services"
  "github.com/yungbote/neurobridge-backend/internal/ssedata"
  "github.com/yungbote/neurobridge-backend/internal/sse"
)

type MaterialHandler struct {
  materialService services.MaterialService
  courseService   services.CourseService
  sseHub          *sse.SSEHub
}

func NewMaterialHandler(msvc services.MaterialService, csvc services.CourseService, sseHub *sse.SSEHub) *MaterialHandler {
  return &MaterialHandler{
    materialService: msvc,
    courseService:   csvc,
    sseHub:          sseHub,
  }
}

func (mh *MaterialHandler) UploadMaterials(c *gin.Context) {
  rd := requestdata.GetRequestData(c.Request.Context())
  if rd == nil || rd.UserID == uuid.Nil {
    c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
    return
  }
  userID := rd.UserID

  if err := c.Request.ParseMultipartForm(32 << 20); err != nil {
    c.JSON(http.StatusBadRequest, gin.H{"error": "invalid multipart form"})
    return
  }

  form := c.Request.MultipartForm
  fileHeaders := form.File["files"]
  if len(fileHeaders) == 0 {
    c.JSON(http.StatusBadRequest, gin.H{"error": "no files uploaded"})
    return
  }

  uploaded := make([]services.UploadedFileInfo, 0, len(fileHeaders))
  defer func() {
    for _, uf := range uploaded {
      if rc, ok := uf.Reader.(io.ReadCloser); ok {
        rc.Close()
      }
    }
  }()

  for _, fh := range fileHeaders {
    mimeHeader := fh.Header.Get("Content-Type")

    // sniff
    sniffFile, err := fh.Open()
    if err != nil {
      c.Logger().Error("cannot open file for sniffing: " + err.Error())
      continue
    }
    buf := make([]byte, 512)
    n, _ := sniffFile.Read(buf)
    sniffFile.Close()
    sniffType := http.DetectContentType(buf[:n])

    mimeType := mimeHeader
    if mimeType == "" {
      mimeType = sniffType
    }

    r, err := fh.Open()
    if err != nil {
      c.Logger().Error("cannot open file for reading: " + err.Error())
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
    c.JSON(http.StatusBadRequest, gin.H{"error": "could not read any files"})
    return
  }

  // 1) MaterialSet + MaterialFiles + bucket uploads
  set, _, err := mh.materialService.UploadMaterialFiles(c.Request.Context(), nil, userID, uploaded)
  if err != nil {
    c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to register material set/files"})
    return
  }

  // 2) Create course (random title/desc) linked to this material set
  _, err = mh.courseService.CreateCourseFromMaterialSet(c.Request.Context(), nil, userID, set.ID)
  if err != nil {
    c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create course from material set"})
    return
  }

  // 3) Flush SSE messages
  ssd := ssedata.GetSSEData(c.Request.Context())
  if ssd != nil && len(ssd.Messages) > 0 {
    for _, msg := range ssd.Messages {
      mh.sseHub.Broadcast(msg)
    }
    ssd.Messages = nil
  }

  c.JSON(http.StatusOK, gin.H{"ok": true})
}










