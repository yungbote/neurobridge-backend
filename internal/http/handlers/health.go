package handlers

import (
	"net/http"
	"github.com/gin-gonic/gin"
)

type HealthHandler struct {}

func NewHealthHandler() *HealthHandler { return &HealthHandler{} }

func (h *HealthHandler) HealthCheck(c *gin.Context) {
	c.String(http.StatusOK, "ok")
}










