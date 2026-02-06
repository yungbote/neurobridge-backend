package handlers

import (
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

type HealthHandler struct{}

func NewHealthHandler() *HealthHandler { return &HealthHandler{} }

func (h *HealthHandler) HealthCheck(c *gin.Context) {
	c.String(http.StatusOK, "ok")
}

func (h *HealthHandler) MetricsHealth(c *gin.Context) {
	if !metricsEnabled() {
		c.String(http.StatusServiceUnavailable, "disabled")
		return
	}
	c.String(http.StatusOK, "ok")
}

func metricsEnabled() bool {
	v := strings.TrimSpace(os.Getenv("METRICS_ENABLED"))
	if v == "" {
		return false
	}
	return strings.EqualFold(v, "true") || v == "1" || strings.EqualFold(v, "yes")
}
