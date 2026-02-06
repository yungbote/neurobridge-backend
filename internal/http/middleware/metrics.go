package middleware

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/yungbote/neurobridge-backend/internal/observability"
)

// Metrics instruments HTTP request counts/latency when metrics are enabled.
func Metrics(m *observability.Metrics) gin.HandlerFunc {
	if m == nil {
		return func(c *gin.Context) { c.Next() }
	}
	return func(c *gin.Context) {
		start := time.Now()
		m.ApiInflightInc()
		defer m.ApiInflightDec()

		c.Next()

		route := c.FullPath()
		if route == "" {
			route = "unknown"
		}
		status := strconv.Itoa(c.Writer.Status())
		method := c.Request.Method
		m.ObserveAPI(method, route, status, time.Since(start))
	}
}
