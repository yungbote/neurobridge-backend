package handlers

import (
	"net/http"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/services"
	"github.com/yungbote/neurobridge-backend/internal/http/response"
)

type JobHandler struct {
	jobs services.JobService
}

func NewJobHandler(jobs services.JobService) *JobHandler {
	return &JobHandler{jobs: jobs}
}

// GET /api/jobs/:id
func (h *JobHandler) GetJob(c *gin.Context) {
	jobID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.RespondError(c, http.StatusBadRequest, "invalid_job_id", err)
		return
	}
	job, err := h.jobs.GetByIDForRequestUser(c.Request.Context(), nil, jobID)
	if err != nil {
		response.RespondError(c, http.StatusBadRequest, "job_not_found", err)
		return
	}

	response.RespondOK(c, gin.H{"job": job})
}










