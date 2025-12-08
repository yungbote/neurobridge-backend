package middleware

import (
  "encoding/json"
  "net/http"
  "strings"
  "github.com/gin-gonic/gin"
  "github.com/google/uuid"
  "github.com/yungbote/neurobridge-backend/internal/logger"
  "github.com/yungbote/neurobridge-backend/internal/requestdata"
  "github.com/yungbote/neurobridge-backend/internal/ssedata"
  "github.com/yungbote/neurobridge-backend/internal/repos"
  "github.com/yungbote/neurobridge-backend/internal/services"
)

type AuthMiddleware struct {
  log           *logger.Logger
  authService   services.AuthService
}

func NewAuthMiddleware(log *logger.Logger, authService services.AuthService) *AuthMiddleware {
  middlewareLogger := log.With("Middleware", "AuthMiddleware")
  return &AuthMiddleware{log: middlewareLogger, authService: authService}
}

func (am *AuthMiddleware) RequireAuth() gin.HandlerFunc {
  return func(c *gin.Context) {
    tokenString := extractTokenFromAll(c)
    am.log.Debug("TokenString:", "tokenstring", tokenString)
    if tokenString == "" {
      c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing or invalid token"})
      return
    }
    ctx, err := am.authService.SetContextFromToken(c.Request.Context(), tokenString)
    if err != nil {
      c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
      return
    }
    ctx = ssedata.WithSSEData(ctx)
    c.Request = c.Request.WithContext(ctx)
    rd := requestdata.GetRequestData(ctx)
    if rd == nil || rd.UserID == uuid.Nil {
      c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "forbidden"})
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
  var body struct {
    Token         string        `json:"token"`
  }
  if err := json.NewDecoder(c.Request.Body).Decode(&body); err != nil {
    if body.Token != "" {
      return body.Token
    }
  }
  return ""
}
