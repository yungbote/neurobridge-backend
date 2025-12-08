package server

import (
  "github.com/gin-gonic/gin"
  "github.com/gin-contrib/cors"
  "github.com/yungbote/neurobridge-backend/internal/handlers"
  "github.com/yungbote/neurobridge-backend/internal/middleware"
)

type RouterConfig struct {
  AuthHandler       *handlers.AuthHandler
  AuthMiddleware    *middleware.AuthMiddleware
  UserHandler       *handlers.UserHandler
  SSEHandler        *handlers.SSEHandler
}

func NewRouter(cfg RouterConfig) *gin.Engine {
  router := gin.Default()

  // Cors
  router.Use(cors.New(cors.Config{
    AllowOrigins: []string{
      "http://localhost:80",
      "http://localhost:3000",
      "http://localhost:5174"
    },
    AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"}
    AllowHeaders:     []string{"Authorization", "Content-Type", "X-Requested-With"},
    AllowCredentials: true,
  }))


// ===============
// || Public    ||
// ===============
  router.GET("/healthcheck", handlers.HealthCheck)
  api := router.Group("/api")
  {
    router.POST("/register", cfg.AuthHandler.Register)
    router.POST("/login", cfg.AuthHandler.Login)
  }

// ===============
// || Protected ||
// ===============
  protected := router.Group("/")
  protected.Use(cfg.AuthMiddleware.RequireAuth())
  // Auth
  protected.POST("/refresh", cfg.AuthHandler.Refresh)
  protected.POST("/logout", cfg.AuthHandler.Logout)
  // SSE
  protected.GET("/sse/stream", cfg.SSEHandler.SSEStream)
  protected.POST("/sse/subscribe", cfg.SSEHandler.SSESubscribe)
  protected.POST("/sse/unsubscribe", cfg.SSEHandler.SSEUnsubscribe)
  // User
  protected.GET("/user", cfg.UserHandler.GetMe)

  return router
}
