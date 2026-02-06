package middleware

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/yungbote/neurobridge-backend/internal/platform/ctxutil"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
)

func RequestLogger(log *logger.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		if log == nil {
			return
		}

		status := c.Writer.Status()
		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}
		td := ctxutil.GetTraceData(c.Request.Context())
		rd := ctxutil.GetRequestData(c.Request.Context())

		fields := []interface{}{
			"method", strings.ToUpper(c.Request.Method),
			"path", path,
			"status", status,
			"duration_ms", time.Since(start).Milliseconds(),
		}
		if td != nil {
			if td.TraceID != "" {
				fields = append(fields, "trace_id", td.TraceID)
			}
			if td.RequestID != "" {
				fields = append(fields, "request_id", td.RequestID)
			}
		}
		if rd != nil {
			if rd.UserID.String() != "" {
				fields = append(fields, "user_id", rd.UserID.String())
			}
			if rd.SessionID.String() != "" {
				fields = append(fields, "session_id", rd.SessionID.String())
			}
		}

		switch {
		case status >= 500:
			log.Error("HTTP request", fields...)
		case status >= 400:
			log.Warn("HTTP request", fields...)
		default:
			log.Info("HTTP request", fields...)
		}
	}
}
