package handlers

import (
	"net/http"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/services"
)

type JobsHandler struct {
	jobs services.JobService
}

func NewJobsHandler(jobs services.JobService) *JobsHandler {
	return &JobsHandler{jobs: jobs}
}

// GET /api/jobs/:id
func (h *JobsHandler) GetJobByID(c *gin.Context) {
	jobID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		RespondError(c, http.StatusBadRequest, "invalid_job_id", err)
		return
	}
	job, err := h.jobs.GetByIDForRequestUser(c.Request.Context(), nil, jobID)
	if err != nil {
		RespondError(c, http.StatusBadRequest, "job_not_found", err)
		return
	}

	RespondOK(c, gin.H{"job": job})
}










