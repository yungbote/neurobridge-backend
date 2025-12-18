package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/yungbote/neurobridge-backend/internal/pkg/ctxutil"
)

func AttachRequestContext() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		ctx = ctxutil.WithSSEData(ctx)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
