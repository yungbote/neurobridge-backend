package middleware

import (
  "github.com/gin-gonic/gin"
  "github.com/yungbote/neurobridge-backend/internal/ssedata"
)

func AttachRequestContext() gin.HandlerFunc {
  return func(c *gin.Context) {
    ctx := c.Request.Context()
    ctx = ssedata.WithSSEData(ctx)
    c.Request = c.Request.WithContext(ctx)
    c.Next()
  }
}










