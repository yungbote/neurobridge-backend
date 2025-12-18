package handlers

import (
	"github.com/gin-gonic/gin"
	"net/http"
)

type HealthHandler struct{}

func NewHealthHandler() *HealthHandler { return &HealthHandler{} }

func (h *HealthHandler) HealthCheck(c *gin.Context) {
	c.String(http.StatusOK, "ok")
}
