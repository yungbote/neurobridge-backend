package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yungbote/neurobridge-backend/internal/observability"
	"github.com/yungbote/neurobridge-backend/internal/platform/ctxutil"
	"github.com/yungbote/neurobridge-backend/internal/platform/logger"
	"github.com/yungbote/neurobridge-backend/internal/services"
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
			if metrics := observability.Current(); metrics != nil {
				metrics.IncSecurityEvent("missing_token")
			}
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{"message": "missing or invalid token", "code": "unauthorized"},
			})
			return
		}
		ctx, err := am.authService.SetContextFromToken(c.Request.Context(), tokenString)
		if err != nil {
			if metrics := observability.Current(); metrics != nil {
				metrics.IncSecurityEvent("invalid_token")
			}
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{"messages": err.Error(), "code": "unauthorized"},
			})
			return
		}
		// NOTE: SSEData is attached by middleware.AttachRequestContext() globally
		c.Request = c.Request.WithContext(ctx)
		rd := ctxutil.GetRequestData(ctx)
		if rd == nil || rd.UserID == uuid.Nil {
			if metrics := observability.Current(); metrics != nil {
				metrics.IncSecurityEvent("forbidden")
			}
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
