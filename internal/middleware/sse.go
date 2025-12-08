package middleware

import (
  "github.com/gin-gonic/gin"
  "github.com/yungbote/neurobridge-backend/internal/ssedata"
  "github.com/yungbote/neurobridge-backend/internal/errordata"
)

func AttachRequestContext() gin.HandlerFunc {
  return func(c *gin.Context) {
    ctx := c.Request.Context()
    ctx = ssedata.WithSSEData(ctx)
    ctx = errordata.WithErrorData(ctx)
    c.Request = c.Request.WithContext(ctx)
    c.Next()
  }
}
