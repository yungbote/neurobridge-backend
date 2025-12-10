package handlers

import (
	"net/http"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/logger"
	"github.com/yungbote/neurobridge-backend/internal/requestdata"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type MaterialHandler struct {
	log							*logger.Logger
	materialService	services.MaterialService
	bucketService		services.BucketService
}

func NewMaterialHandler(log *logger.Logger, msvc service.MaterialService, bsvc services.BucketService) *MaterialHandler {
	return &MaterialHandler{
		log:							log.With("handler", "MaterialHandler"),
		materialService		msvc,
		bucketService			bsvc,
	}
}

// POST /api/material-sets/upload-and-generate
// Upload batch and kick off full pipeline (analyze -> plan -> generate course -> generate lessons)
func (h *MaterialHandler) UploadAndGenerateCourse(c *gin.Context) {
	c.Status(http.StatusNotImplemented)
}

// POST /api/material-sets/upload
// Just upload batch and create MaterialSet + MaterialFiles (no pipeline)
func (h *MaterialHandler) UploadMaterials(c *gin.Context) {
	c.Status(http.StatusNotImplemented)
}

// GET /api/material-sets
// List this user's material sets, optionally filtered by status
func (h *MaterialHandler) ListMaterialSets(c *gin.Context) {
	c.Status(http.StatusNotImplemented)
}

// GET /api/material-sets/:id
// Detailed view of a material set + its files and pipelines status
func (h *MaterialHandler) GetMaterialSet(c *gin.Context) {
	c.Status(http.StatusNotImplemented)
}

// DELETE /api/material-sets/:id
// Soft-delete a material set (and cascade logic in services)
func (h *MaterialHandler) DeleteMaterialSet(c *gin.Context) {
	c.Status(http.StatusNotImplemented)
}

// GET /api/material-sets/:id/files
func (h *MaterialHandler) ListMaterialFiles(c *gin.Context) {
	c.Status(http.StatusNotImplemented)
}

// GET /api/material-files/:id
func (h *MaterialHandler) GetMaterialFile(c *gin.Context) {
	c.Status(http.StatusNotImplemented)
}

// DELETE /api/material-files/:id
func (h *MaterialHandler) DeleteMaterialFile(c *gin.Context) {
	c.Status(http.StatusNotImplemented)
}










