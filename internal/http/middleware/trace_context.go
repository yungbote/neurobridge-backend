package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"

	"github.com/yungbote/neurobridge-backend/internal/platform/ctxutil"
)

const (
	headerTraceID   = "X-Trace-Id"
	headerRequestID = "X-Request-Id"
)

func AttachTraceContext() gin.HandlerFunc {
	return func(c *gin.Context) {
		reqID := strings.TrimSpace(c.GetHeader(headerRequestID))
		if reqID == "" {
			reqID = uuid.New().String()
		}
		traceID := strings.TrimSpace(c.GetHeader(headerTraceID))
		if traceID == "" {
			spanCtx := trace.SpanContextFromContext(c.Request.Context())
			if spanCtx.HasTraceID() {
				traceID = spanCtx.TraceID().String()
			}
		}
		if traceID == "" {
			traceID = uuid.New().String()
		}
		ctx := ctxutil.WithTraceData(c.Request.Context(), &ctxutil.TraceData{
			TraceID:   traceID,
			RequestID: reqID,
		})
		c.Request = c.Request.WithContext(ctx)
		c.Set("trace_id", traceID)
		c.Set("request_id", reqID)
		c.Writer.Header().Set(headerTraceID, traceID)
		c.Writer.Header().Set(headerRequestID, reqID)
		c.Next()
	}
}
