package handlers

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/http/response"
	"github.com/yungbote/neurobridge-backend/internal/services"
	"net/http"
	"strings"
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

// POST /api/jobs/:id/cancel
func (h *JobHandler) CancelJob(c *gin.Context) {
	jobID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.RespondError(c, http.StatusBadRequest, "invalid_job_id", err)
		return
	}
	job, err := h.jobs.CancelForRequestUser(c.Request.Context(), nil, jobID)
	if err != nil {
		response.RespondError(c, http.StatusBadRequest, "cancel_job_failed", err)
		return
	}
	response.RespondOK(c, gin.H{"job": job})
}

// POST /api/jobs/:id/restart
func (h *JobHandler) RestartJob(c *gin.Context) {
	jobID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.RespondError(c, http.StatusBadRequest, "invalid_job_id", err)
		return
	}
	job, err := h.jobs.RestartForRequestUser(c.Request.Context(), nil, jobID)
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(strings.ToLower(err.Error()), "not restartable") {
			status = http.StatusConflict
		}
		response.RespondError(c, status, "restart_job_failed", err)
		return
	}
	response.RespondOK(c, gin.H{"job": job})
}
