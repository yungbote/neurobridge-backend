package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/yungbote/neurobridge-backend/internal/pkg/ctxutil"
	"github.com/yungbote/neurobridge-backend/internal/pkg/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
	"net/http"
	"strings"
)

type AuthMiddleware struct {
	log         *logger.Logger
	authService services.AuthService
}

func NewAuthMiddleware(log *logger.Logger, authService services.AuthService) *AuthMiddleware {
	middlewareLogger := log.With("Middleware", "AuthMiddleware")
	return &AuthMiddleware{log: middlewareLogger, authService: authService}
}

func (am *AuthMiddleware) RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenString := extractTokenFromAll(c)
		am.log.Debug("TokenString", "token", tokenString)
		if tokenString == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{"message": "missing or invalid token", "code": "unauthorized"},
			})
			return
		}
		ctx, err := am.authService.SetContextFromToken(c.Request.Context(), tokenString)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{"messages": err.Error(), "code": "unauthorized"},
			})
			return
		}
		// NOTE: SSEData is attached by middleware.AttachRequestContext() globally
		c.Request = c.Request.WithContext(ctx)
		rd := ctxutil.GetRequestData(ctx)
		if rd == nil || rd.UserID == uuid.Nil {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": gin.H{"message": "forbidden", "code": "forbidden"},
			})
			return
		}
		c.Next()
	}
}

func extractTokenFromAll(c *gin.Context) string {
	if qToken := c.Query("token"); qToken != "" {
		return qToken
	}
	authHeader := c.GetHeader("Authorization")
	if len(authHeader) > 7 && strings.EqualFold(authHeader[:7], "Bearer ") {
		return authHeader[7:]
	}
	return ""
}
